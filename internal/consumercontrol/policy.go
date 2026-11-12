// Package consumercontrol содержит чистые правила допуска. Без транзакционного
// хранилища эти правила не являются межпроцессной блокировкой.
package consumercontrol

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"math"
	"regexp"
)

type Mode string

// MaxOwnerLeaseMS ограничивает предложенный lease пятью минутами.
const MaxOwnerLeaseMS int64 = 5 * 60 * 1000

const (
	Legacy   Mode = "legacy"
	Draining Mode = "draining"
	Worker   Mode = "worker"
)

type Origin string

const (
	LegacyTick Origin = "legacy_tick"
	Manual     Origin = "manual"
	Diagnostic Origin = "diagnostic"
	WorkerRun  Origin = "worker"
)

var (
	ErrState      = errors.New("invalid consumer control state")
	ErrStale      = errors.New("consumer control generation or revision changed")
	ErrDenied     = errors.New("consumer start is not permitted")
	ErrTransition = errors.New("consumer transition is not permitted")
)

var generationPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Token не сериализует и не форматирует credential; хранить разрешено только digest.
type Token struct{ value [32]byte }

func NewToken() (Token, error) {
	var token Token
	if _, err := rand.Read(token.value[:]); err != nil {
		return Token{}, err
	}
	if token.value == ([32]byte{}) {
		return Token{}, ErrState
	}
	return token, nil
}

func (Token) String() string               { return "<redacted>" }
func (Token) GoString() string             { return "<redacted>" }
func (Token) MarshalJSON() ([]byte, error) { return []byte(`"<redacted>"`), nil }

func (t Token) digest() ([32]byte, error) {
	if t.value == ([32]byte{}) {
		return [32]byte{}, ErrState
	}
	return sha256.Sum256(t.value[:]), nil
}

// Digest предназначен для хранения вместо credential; digest не является Token.
func (t Token) Digest() ([32]byte, error) { return t.digest() }

// State — проверенный снимок из БД. OwnerDigest не является credential для запуска.
type State struct {
	Generation   string
	Revision     uint64
	Mode         Mode
	OwnerDigest  [32]byte
	LeaseUntilMS int64
}

func validate(s State) error {
	if !generationPattern.MatchString(s.Generation) || s.Revision == 0 || s.Revision > math.MaxInt64 {
		return ErrState
	}
	hasOwner := s.OwnerDigest != ([32]byte{})
	switch s.Mode {
	case Legacy:
		if hasOwner || s.LeaseUntilMS != 0 {
			return ErrState
		}
	case Draining:
		if (!hasOwner && s.LeaseUntilMS != 0) || (hasOwner && s.LeaseUntilMS <= 0) {
			return ErrState
		}
	case Worker:
		if !hasOwner || s.LeaseUntilMS <= 0 {
			return ErrState
		}
	default:
		return ErrState
	}
	return nil
}

// Request хранит секрет только в непрозрачном Token и не должен логироваться.
type Request struct {
	Generation string
	Revision   uint64
	Origin     Origin
	Token      Token
}

// CanStart проверяет только новый запуск. Снимок и часы должны быть получены
// в той же транзакции, что создаёт run; завершение текущих leases — отдельно.
func CanStart(s State, req Request, nowMS int64) error {
	if err := validate(s); err != nil || nowMS <= 0 {
		return ErrState
	}
	if req.Generation != s.Generation || req.Revision != s.Revision {
		return ErrStale
	}
	switch s.Mode {
	case Legacy:
		if req.Origin == LegacyTick || req.Origin == Manual || req.Origin == Diagnostic {
			return nil
		}
	case Worker:
		if req.Origin != WorkerRun || s.LeaseUntilMS <= nowMS || s.LeaseUntilMS-nowMS > MaxOwnerLeaseMS {
			return ErrDenied
		}
		digest, err := req.Token.digest()
		if err == nil && subtle.ConstantTimeCompare(digest[:], s.OwnerDigest[:]) == 1 {
			return nil
		}
	}
	return ErrDenied
}

// CanStartRun дополнительно ограничивает срок нового активного run.
// Для worker срок не может пережить owner lease. Это не проверка job lease.
func CanStartRun(s State, req Request, runUntilMS, nowMS int64) error {
	if err := CanStart(s, req, nowMS); err != nil {
		return err
	}
	if runUntilMS <= nowMS || (s.Mode == Worker && runUntilMS > s.LeaseUntilMS) {
		return ErrDenied
	}
	return nil
}

// CanFinish проверяет только consumer-владельца уже начатой работы.
// Caller обязан отдельно доказать в той же транзакции, что run/job были
// захвачены этим владельцем до draining, их lease и generation действительны,
// и повторить проверки непосредственно перед commit. Этот метод не даёт
// права на новый claim, запуск провайдера или запись произвольного результата.
// Он не подключён к runtime и сам по себе не является SQL-fence.
func CanFinish(s State, req Request, nowMS int64) error {
	if err := validate(s); err != nil || nowMS <= 0 {
		return ErrState
	}
	if req.Generation != s.Generation {
		return ErrStale
	}
	if req.Origin != WorkerRun || req.Revision == 0 ||
		(s.Mode != Worker && s.Mode != Draining) ||
		s.LeaseUntilMS <= nowMS || s.LeaseUntilMS-nowMS > MaxOwnerLeaseMS {
		return ErrDenied
	}
	if (s.Mode == Worker && req.Revision != s.Revision) ||
		(s.Mode == Draining && (s.Revision <= 1 || req.Revision != s.Revision-1)) {
		return ErrStale
	}
	digest, err := req.Token.digest()
	if err != nil || subtle.ConstantTimeCompare(digest[:], s.OwnerDigest[:]) != 1 {
		return ErrDenied
	}
	return nil
}

// Change описывает решение доверенного операционного слоя. Quiescent и
// EvidenceVerified должны проверяться в той же SQL-транзакции; bool не является доказательством сам по себе.
// Для каждого нового назначения worker вызывающий обязан создать новый случайный Token,
// в том числе после промежуточного возврата в legacy.
type Change struct {
	Generation       string
	Revision         uint64
	To               Mode
	Owner            Token
	LeaseUntilMS     int64
	Quiescent        bool
	EvidenceVerified bool
}

// Renew рассчитывает продление только живого worker-владельца. Revision не
// меняется: продление не должно лишать допуска уже захваченные им задания.
// Переход в draining меняет revision и запрещает последующее продление.
func Renew(s State, req Request, untilMS, nowMS int64) (State, error) {
	if err := CanStart(s, req, nowMS); err != nil {
		return State{}, err
	}
	if s.Mode != Worker || untilMS <= s.LeaseUntilMS || untilMS <= nowMS || untilMS-nowMS > MaxOwnerLeaseMS {
		return State{}, ErrDenied
	}
	s.LeaseUntilMS = untilMS
	return s, nil
}

// Transition рассчитывает новый снимок без записи; caller обязан применить CAS.
func Transition(s State, change Change, nowMS int64) (State, error) {
	if err := validate(s); err != nil || nowMS <= 0 || s.Revision == math.MaxInt64 {
		return State{}, ErrState
	}
	if change.Generation != s.Generation || change.Revision != s.Revision {
		return State{}, ErrStale
	}
	next := s
	switch {
	case s.Mode == Legacy && change.To == Draining, s.Mode == Worker && change.To == Draining:
		if change.Owner.value != ([32]byte{}) || change.LeaseUntilMS != 0 {
			return State{}, ErrTransition
		}
		next.Mode = Draining
	case s.Mode == Draining && change.To == Worker:
		if !change.Quiescent || !change.EvidenceVerified || change.LeaseUntilMS <= nowMS || change.LeaseUntilMS-nowMS > MaxOwnerLeaseMS {
			return State{}, ErrTransition
		}
		digest, err := change.Owner.digest()
		if err != nil {
			return State{}, ErrTransition
		}
		// При повторном назначении нельзя возродить credential прежнего worker.
		if s.OwnerDigest != ([32]byte{}) && subtle.ConstantTimeCompare(digest[:], s.OwnerDigest[:]) == 1 {
			return State{}, ErrTransition
		}
		next.Mode, next.OwnerDigest, next.LeaseUntilMS = Worker, digest, change.LeaseUntilMS
	case s.Mode == Draining && change.To == Legacy:
		if !change.Quiescent || !change.EvidenceVerified || change.Owner.value != ([32]byte{}) || change.LeaseUntilMS != 0 {
			return State{}, ErrTransition
		}
		next.Mode, next.OwnerDigest, next.LeaseUntilMS = Legacy, [32]byte{}, 0
	default:
		return State{}, ErrTransition
	}
	next.Revision++
	return next, nil
}
