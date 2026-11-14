package workerloop

import (
	"context"
	"math/rand"
	"time"
)

// ProfileName — внутренняя очередь; не извлекается из пользовательского payload.
type ProfileName string

const (
	Launch    ProfileName = "launch"
	Reconcile ProfileName = "reconcile"
	Pipeline  ProfileName = "pipeline"
)

// Границы локального polling не являются provider rate limit или SLA.
const minProfileDelay = 10 * time.Millisecond
const maxProfileDelay = 24 * time.Hour
const maxProfileStep = 5 * time.Minute

// ProfileOutcome не содержит ошибок, payload или учётных данных.
type ProfileOutcome string

const (
	ProfileWorked  ProfileOutcome = "worked"
	ProfileIdle    ProfileOutcome = "idle"
	ProfileFailed  ProfileOutcome = "failed"
	ProfileTimeout ProfileOutcome = "timeout"
)

// Profile задаёт ограниченную последовательную итерацию и интервалы ожидания.
// Step обязан соблюдать контекст и собственные бюджетные/paid guards.
type Profile struct {
	Name                                     ProfileName
	Step                                     Step
	WorkDelay, IdleDelay, ErrorMin, ErrorMax time.Duration
	StepTimeout                              time.Duration
	// OnResult должен быть быстрым и не выполнять сетевых запросов.
	OnResult func(ProfileName, ProfileOutcome)
}

type profileSlot struct {
	profile Profile
	next    time.Time
	backoff time.Duration
}

// profilePlan принадлежит одной горутине. Он не представляет distributed lock.
type profilePlan struct {
	slots  []profileSlot
	cursor int
}

func newProfilePlan(profiles []Profile) (*profilePlan, error) {
	if len(profiles) == 0 || len(profiles) > 3 {
		return nil, ErrConfig
	}
	seen := make(map[ProfileName]bool, len(profiles))
	p := &profilePlan{}
	for _, v := range profiles {
		if (v.Name != Launch && v.Name != Reconcile && v.Name != Pipeline) || seen[v.Name] || v.Step == nil || v.WorkDelay < minProfileDelay || v.WorkDelay > maxProfileDelay || v.IdleDelay < minProfileDelay || v.IdleDelay > maxProfileDelay || v.ErrorMin < minProfileDelay || v.ErrorMax < v.ErrorMin || v.ErrorMax > maxProfileDelay || v.StepTimeout < minProfileDelay || v.StepTimeout > maxProfileStep {
			return nil, ErrConfig
		}
		seen[v.Name] = true
		p.slots = append(p.slots, profileSlot{profile: v, backoff: v.ErrorMin})
	}
	return p, nil
}

// ValidateProfiles проверяет параметры до открытия соединений и запуска шагов.
func ValidateProfiles(profiles []Profile) error {
	_, err := newProfilePlan(profiles)
	return err
}

// profileJitter не сокращает ожидание и не превышает заданный предел.
func profileJitter(delay, limit time.Duration) time.Duration {
	spread := delay / 4
	if available := limit - delay; spread > available {
		spread = available
	}
	if spread <= 0 {
		return delay
	}
	return delay + time.Duration(rand.Int63n(int64(spread)+1))
}

// nextReady возвращает профиль по кругу либо время до ближайшей попытки.
// Отложенный или ошибочный профиль не препятствует готовым очередям.
func (p *profilePlan) nextReady(now time.Time) (int, time.Duration) {
	var earliest time.Time
	for n := 0; n < len(p.slots); n++ {
		i := (p.cursor + n) % len(p.slots)
		when := p.slots[i].next
		if when.IsZero() || !when.After(now) {
			p.cursor = (i + 1) % len(p.slots)
			return i, 0
		}
		if earliest.IsZero() || when.Before(earliest) {
			earliest = when
		}
	}
	return -1, earliest.Sub(now)
}

func (p *profilePlan) complete(i int, now time.Time, worked bool, failed bool) {
	s := &p.slots[i]
	var delay time.Duration
	if failed {
		delay = s.backoff
		if s.backoff > s.profile.ErrorMax/2 {
			s.backoff = s.profile.ErrorMax
		} else {
			s.backoff *= 2
		}
	} else {
		s.backoff = s.profile.ErrorMin
		if worked {
			delay = s.profile.WorkDelay
		} else {
			delay = s.profile.IdleDelay
		}
	}
	s.next = now.Add(delay)
}

// RunProfiles выполняет не более одного шага одновременно. Готовые очереди
// обходятся по кругу; сбой одной имеет собственный bounded backoff. Ожидание и
// шаг отменяются родителем. Ошибка не выводится в лог и не передаётся callback,
// чтобы случайно не раскрыть DSN или данные провайдера. Не подключён к runtime.
// Step должен соблюдать дедлайн: принудительно остановить игнорирующую его
// функцию без риска параллельной работы нельзя. Конфигурация не
// заменяет durable rate limits, budget, fencing и единственность consumer.
func RunProfiles(ctx context.Context, profiles []Profile, onFailure func(ProfileName)) error {
	return RunProfilesWithGrace(ctx, profiles, onFailure, 0)
}

// RunProfilesWithGrace прекращает запуск новых шагов при отмене и даёт
// текущему шагу до grace для завершения. Step обязан соблюдать свой контекст.
func RunProfilesWithGrace(ctx context.Context, profiles []Profile, onFailure func(ProfileName), grace time.Duration) error {
	if ctx == nil {
		return ErrConfig
	}
	if grace < 0 || grace > 30*time.Second {
		return ErrConfig
	}
	p, err := newProfilePlan(profiles)
	if err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		i, wait := p.nextReady(time.Now())
		if i < 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		profile := p.slots[i].profile
		stepParent := ctx
		if grace > 0 {
			stepParent = context.WithoutCancel(ctx)
		}
		stepCtx, cancel := context.WithTimeout(stepParent, profile.StepTimeout)
		if grace > 0 {
			go func() {
				select {
				case <-ctx.Done():
					timer := time.NewTimer(grace)
					defer timer.Stop()
					select {
					case <-timer.C:
						cancel()
					case <-stepCtx.Done():
					}
				case <-stepCtx.Done():
				}
			}()
		}
		worked, stepErr := profile.Step(stepCtx)
		stepContextErr := stepCtx.Err()
		cancel()
		if err := ctx.Err(); err != nil {
			return err
		}
		outcome := ProfileIdle
		failed := stepErr != nil || stepContextErr != nil
		switch {
		case stepContextErr == context.DeadlineExceeded:
			outcome = ProfileTimeout
		case failed:
			outcome = ProfileFailed
		case worked:
			outcome = ProfileWorked
		}
		now := time.Now()
		p.complete(i, now, worked, failed)
		limit := maxProfileDelay
		if failed {
			limit = profile.ErrorMax
		}
		p.slots[i].next = now.Add(profileJitter(p.slots[i].next.Sub(now), limit))
		if profile.OnResult != nil {
			profile.OnResult(profile.Name, outcome)
		}
		if failed && onFailure != nil {
			onFailure(profile.Name)
		}
	}
}
