package coverletters

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

// paidBilling допускает один OpenAI POST на снимок письма. Существующий
// openrouter_ai_logs хранит долговечное свидетельство ДО сетевого вызова:
// стоимость и наличие фактического списания остаются неизвестными.
// Не удалять и не переквалифицировать unknown при таймауте, lost ACK или restore.
type paidBilling struct {
	mu    sync.Mutex
	repo  Repository
	lease Lease
	model string
	used  bool
}

func paidLetterFingerprint(l Lease) string {
	sum := sha256.Sum256([]byte(l.Generation + "\x00" + l.User + "\x00" + l.InputHash))
	return hex.EncodeToString(sum[:])
}

func (b *paidBilling) Run(ctx context.Context, request json.RawMessage, work func(context.Context) (json.RawMessage, error)) (json.RawMessage, error) {
	if b == nil || work == nil {
		return nil, ErrUnavailable
	}
	var meta struct {
		Provider string          `json:"provider"`
		Model    string          `json:"model"`
		Payload  json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(request, &meta) != nil || meta.Provider != "openai" ||
		strings.TrimSpace(meta.Model) == "" || len(meta.Payload) == 0 ||
		(b.model != "" && meta.Model != b.model) {
		return nil, ErrPaidBilling
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used {
		return nil, ErrPaidUncertain
	}
	// Даже при локальной ошибке после попытки admission нет автоматического
	// второго вызова в рамках одной генерации.
	b.used = true
	if b.repo.DB == nil {
		return nil, ErrUnavailable
	}
	if err := b.repo.Check(ctx, b.lease); err != nil {
		return nil, err
	}

	// Сериализуем admission общим generation lock. Это не SQL-транзакция
	// вокруг POST: блокировка снимается ДО сети.
	tx, err := b.repo.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, ErrUnavailable
	}
	defer tx.Rollback()
	var generation string
	if err := tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation); err != nil {
		return nil, ErrUnavailable
	}
	if generation != b.lease.Generation {
		return nil, ErrChanged
	}
	var inputHash, token string
	var expires int64
	if err := tx.QueryRowContext(ctx,
		"SELECT input_hash,owner_token,expires_at FROM cover_letter_generation_leases WHERE user_name=? AND vacancy_id=? FOR UPDATE",
		b.lease.User, b.lease.VacancyID).Scan(&inputHash, &token, &expires); err != nil {
		return nil, ErrChanged
	}
	if inputHash != b.lease.InputHash || token != b.lease.Token || expires <= time.Now().UnixMilli() {
		return nil, ErrChanged
	}
	current, err := readInput(ctx, tx, b.lease.User, b.lease.VacancyID)
	if err != nil || current.Hash() != b.lease.InputHash {
		return nil, ErrChanged
	}

	fingerprint := paidLetterFingerprint(b.lease)
	var exists int
	// Суффикс не изменяется ни при получении ответа, ни при отмене.
	// В старых free-логах его нет. После commit с потерянным ACK новая
	// попытка увидит запись и не отправит повторный оплачиваемый POST.
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM openrouter_ai_logs WHERE task='openai_cover_letter' AND RIGHT(COALESCE(error_code,''),65)=CONCAT(':',?)",
		fingerprint).Scan(&exists); err != nil {
		return nil, ErrUnavailable
	}
	if exists != 0 {
		return nil, ErrPaidUncertain
	}
	row, err := tx.ExecContext(ctx,
		"INSERT INTO openrouter_ai_logs(created_at,task,requested_primary_model,success,error_code) VALUES(UTC_TIMESTAMP(),'openai_cover_letter',?,0,?)",
		meta.Model, "paid_unknown:"+fingerprint)
	if err != nil {
		return nil, ErrUnavailable
	}
	id, err := row.LastInsertId()
	if err != nil {
		return nil, ErrUnavailable
	}
	if err := tx.Commit(); err != nil {
		// COMMIT мог состояться. Ни работы, ни нового reserve/POST.
		return nil, ErrUnavailable
	}
	// Любая отмена после commit оставляет unknown (в том числе если
	// физически POST ещё не отправлялся): повтор разрешит только сверка.
	if err := b.repo.Check(ctx, b.lease); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	raw, callErr := work(ctx)
	// Наличие ответа — не доказанная стоимость и не доказательство Save.
	// Метаданные без промта, резюме и текста. Даже сбой UPDATE не стирает
	// предшествующий durable unknown и не разрешает повтор.
	status, code := 0, "paid_unknown:"+fingerprint
	if callErr == nil {
		status, code = 1, "paid_response:"+fingerprint
	}
	finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, recordErr := b.repo.DB.ExecContext(finishCtx,
		"UPDATE openrouter_ai_logs SET success=?,error_code=? WHERE id=? AND task='openai_cover_letter'",
		status, code, id)
	if recordErr != nil {
		return nil, errors.Join(ErrUnavailable, callErr)
	}
	return raw, callErr
}
