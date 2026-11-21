package workerengine

import (
	"context"
	"errors"
	"sync"
)

// PauseGate сериализует допуск профиля и запрос на паузу внутри экземпляра.
type PauseGate struct {
	mu      sync.Mutex
	token   string
	active  bool
	claims  *claimSignal
	resumed chan struct{}
	unsafe  bool
}

var ErrPauseConflict = errors.New("worker pause token conflict")

type PauseState string

const (
	PauseDraining PauseState = "draining"
	PauseHeld     PauseState = "held"
	PauseUnsafe   PauseState = "unsafe"
)

func NewPauseGate() *PauseGate {
	return &PauseGate{}
}

// Pause закрывает допуск новых профилей и claims. Текущий контекст не отменяется.
func (g *PauseGate) Pause(token string) (PauseState, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if token == "" || (g.token != "" && g.token != token) {
		return "", ErrPauseConflict
	}
	if g.token == "" {
		g.token = token
		g.resumed = make(chan struct{})
		if g.active {
			g.claims.stop()
		}
	}
	switch {
	case g.active:
		return PauseDraining, nil
	case g.unsafe:
		return PauseUnsafe, nil
	default:
		return PauseHeld, nil
	}
}

func (g *PauseGate) Resume(token string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if token == "" || token != g.token {
		return ErrPauseConflict
	}
	g.token = ""
	close(g.resumed)
	return nil
}

func (g *PauseGate) enter(ctx context.Context) (*claimSignal, error) {
	for {
		g.mu.Lock()
		if err := ctx.Err(); err != nil {
			g.mu.Unlock()
			return nil, err
		}
		if g.token == "" {
			g.active = true
			g.claims = &claimSignal{done: make(chan struct{})}
			claims := g.claims
			g.mu.Unlock()
			return claims, nil
		}
		resumed := g.resumed
		g.mu.Unlock()
		select {
		case <-resumed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (g *PauseGate) leave(safe bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.active = false
	g.claims = nil
	// Следующий успешный профиль не подтверждает исход предыдущей ошибки.
	g.unsafe = g.unsafe || !safe
}

type claimSignal struct {
	once sync.Once
	done chan struct{}
}

func (s *claimSignal) stop() {
	s.once.Do(func() { close(s.done) })
}
