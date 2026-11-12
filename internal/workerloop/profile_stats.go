package workerloop

import (
	"math"
	"sync"
)

// ProfileStats — локальные счётчики исходов без payload, ID и текста ошибок.
// Они не измеряют состояние БД, backlog, расходы или вызовы провайдера.
type ProfileStats struct {
	Name     ProfileName `json:"name"`
	Worked   uint64      `json:"worked"`
	Idle     uint64      `json:"idle"`
	Failed   uint64      `json:"failed"`
	TimedOut uint64      `json:"timedOut"`
}

// ProfileCounters можно передать как OnResult; безопасен для чтения health.
type ProfileCounters struct {
	mu     sync.Mutex
	values map[ProfileName]ProfileStats
}

func increment(v *uint64) {
	if *v < math.MaxUint64 {
		*v = *v + 1
	}
}

// Record игнорирует неизвестные значения и не хранит ошибки или данные job.
func (c *ProfileCounters) Record(name ProfileName, outcome ProfileOutcome) {
	if c == nil || (name != Launch && name != Reconcile && name != Pipeline) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.values == nil {
		c.values = make(map[ProfileName]ProfileStats)
	}
	v := c.values[name]
	v.Name = name
	switch outcome {
	case ProfileWorked:
		increment(&v.Worked)
	case ProfileIdle:
		increment(&v.Idle)
	case ProfileFailed:
		increment(&v.Failed)
	case ProfileTimeout:
		increment(&v.TimedOut)
	default:
		return
	}
	c.values[name] = v
}

// Snapshot возвращает независимую копию в стабильном порядке.
func (c *ProfileCounters) Snapshot() []ProfileStats {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]ProfileStats, 0, 3)
	for _, name := range []ProfileName{Launch, Reconcile, Pipeline} {
		v := c.values[name]
		v.Name = name
		result = append(result, v)
	}
	return result
}
