// Package workerhealth хранит локальное состояние будущего worker без запуска службы.
package workerhealth

import (
	"errors"
	"sync"
	"time"
)

// Reason — ограниченный набор кодов, не содержащий сообщений провайдеров или БД.
type Reason string

const (
	ReasonStarting            Reason = "starting"
	ReasonReady               Reason = "ready"
	ReasonStopped             Reason = "stopped"
	ReasonProgressStale       Reason = "progress_stale"
	ReasonDatabaseUnchecked   Reason = "database_unchecked"
	ReasonDatabaseStale       Reason = "database_stale"
	ReasonDatabaseUnavailable Reason = "database_unavailable"
	ReasonWorkFailed          Reason = "work_failed"
	ReasonWorkTimeout         Reason = "work_timeout"
)

// Snapshot — только сведения о прогрессе и последней проверке БД, без ошибок и данных очереди.
type Snapshot struct {
	Live   bool   `json:"live"`
	Ready  bool   `json:"ready"`
	Reason Reason `json:"reason"`
}

// State безопасен при одновременной записи и чтении из нескольких горутин.
type State struct {
	mu                             sync.Mutex
	maxProgressAge, maxDatabaseAge time.Duration
	progressAt, databaseAt         time.Time
	databaseOK, started, stopped   bool
	workFailure                    Reason
	now                            func() time.Time
}

// StartOnce исключает повторное использование состояния для второго локального
// supervisor. Это не межпроцессный lock и не заменяет consumer fence.
func (s *State) StartOnce() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.stopped {
		return false
	}
	s.started = true
	return true
}

// WorkResult фиксирует только ограниченный исход, без текста ошибок.
// Он не заменяет проверку БД и не подтверждает внешние операции.
func (s *State) WorkResult(ok, timeout bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.workFailure = ""
	if timeout {
		s.workFailure = ReasonWorkTimeout
	} else if !ok {
		s.workFailure = ReasonWorkFailed
	}
}

// New создаёт состояние, изначально не готовое к работе.
func New(maxProgressAge, maxDatabaseAge time.Duration) (*State, error) {
	if maxProgressAge <= 0 || maxDatabaseAge <= 0 {
		return nil, errors.New("worker health freshness must be positive")
	}
	return &State{maxProgressAge: maxProgressAge, maxDatabaseAge: maxDatabaseAge, now: time.Now}, nil
}

// Progress вызывается самой рабочей петлёй при продвижении, а не отдельным таймером.
func (s *State) Progress() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stopped {
		s.progressAt = s.now()
	}
}

// DatabaseCheck фиксирует только результат проверки, не принимая текст ошибки.
func (s *State) DatabaseCheck(ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stopped {
		s.databaseAt = s.now()
		s.databaseOK = ok
	}
}

// Stop необратимо помечает экземпляр остановленным.
func (s *State) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
}

func fresh(now, then time.Time, maxAge time.Duration) bool {
	if then.IsZero() {
		return false
	}
	age := now.Sub(then)
	return age >= 0 && age <= maxAge
}

// Snapshot не проверяет БД и не гарантирует доступность внешнего провайдера.
func (s *State) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return Snapshot{Reason: ReasonStopped}
	}
	if s.progressAt.IsZero() {
		return Snapshot{Reason: ReasonStarting}
	}
	now := s.now()
	if !fresh(now, s.progressAt, s.maxProgressAge) {
		return Snapshot{Reason: ReasonProgressStale}
	}
	if s.databaseAt.IsZero() {
		return Snapshot{Live: true, Reason: ReasonDatabaseUnchecked}
	}
	if !fresh(now, s.databaseAt, s.maxDatabaseAge) {
		return Snapshot{Live: true, Reason: ReasonDatabaseStale}
	}
	if !s.databaseOK {
		return Snapshot{Live: true, Reason: ReasonDatabaseUnavailable}
	}
	if s.workFailure != "" {
		return Snapshot{Live: true, Reason: s.workFailure}
	}
	return Snapshot{Live: true, Ready: true, Reason: ReasonReady}
}
