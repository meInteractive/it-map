package workerloop

import (
	"context"
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

// Profile задаёт ограниченную последовательную итерацию и интервалы ожидания.
// Step обязан соблюдать контекст и собственные бюджетные/paid guards.
type Profile struct {
	Name                                     ProfileName
	Step                                     Step
	WorkDelay, IdleDelay, ErrorMin, ErrorMax time.Duration
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
		if (v.Name != Launch && v.Name != Reconcile && v.Name != Pipeline) || seen[v.Name] || v.Step == nil || v.WorkDelay < minProfileDelay || v.WorkDelay > maxProfileDelay || v.IdleDelay < minProfileDelay || v.IdleDelay > maxProfileDelay || v.ErrorMin < minProfileDelay || v.ErrorMax < v.ErrorMin || v.ErrorMax > maxProfileDelay {
			return nil, ErrConfig
		}
		seen[v.Name] = true
		p.slots = append(p.slots, profileSlot{profile: v, backoff: v.ErrorMin})
	}
	return p, nil
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
// Честный обход зависит от ограниченного по времени Step; конфигурация не
// заменяет durable rate limits, budget, fencing и единственность consumer.
func RunProfiles(ctx context.Context, profiles []Profile, onFailure func(ProfileName)) error {
	if ctx == nil {
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
		worked, stepErr := p.slots[i].profile.Step(ctx)
		if err := ctx.Err(); err != nil {
			return err
		}
		p.complete(i, time.Now(), worked, stepErr != nil)
		if stepErr != nil && onFailure != nil {
			onFailure(p.slots[i].profile.Name)
		}
	}
}
