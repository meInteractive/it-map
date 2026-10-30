// Package aiproviders хранит ключи провайдеров в существующей схеме.
// Provider calls, HTTP authorization и запуск parser остаются отдельными слоями.
package aiproviders

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"itmap/internal/integrations/aihttp"
	"itmap/internal/legacycrypto"
)

var ErrStorage = errors.New("provider settings storage unavailable")
var ErrGeneration = errors.New("provider settings generation changed")
var ErrDecrypt = errors.New("provider key cannot be decrypted; save the key again")
var ErrMissing = errors.New("provider key not configured")

type InputError struct {
	Status  int
	Message string
}

func (e *InputError) Error() string { return e.Message }
func invalid(p aihttp.Provider) error {
	name := "OpenAI (sk-…)"
	if p == aihttp.OpenRouter {
		name = "OpenRouter (sk-or-…)"
	}
	return &InputError{400, "Введите корректный API-ключ " + name + "."}
}

var keyPattern = regexp.MustCompile(`^sk-[A-Za-z0-9_-]{12,512}$`)
var routerKeyPattern = regexp.MustCompile(`^sk-or-[A-Za-z0-9_-]{12,512}$`)

func validProvider(p aihttp.Provider) bool { return p == aihttp.OpenAI || p == aihttp.OpenRouter }
func ValidateKey(p aihttp.Provider, key string) error {
	if !validProvider(p) {
		return ErrStorage
	}
	ok := keyPattern.MatchString(key) && !strings.HasPrefix(key, "sk-or-")
	if p == aihttp.OpenRouter {
		ok = routerKeyPattern.MatchString(key)
	}
	if !ok {
		return invalid(p)
	}
	return nil
}
func ParseKey(p aihttp.Provider, body []byte) (string, error) {
	if len(body) > 2048 {
		return "", &InputError{413, "Слишком длинный ключ."}
	}
	body = bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf})
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return "", &InputError{400, "Некорректный JSON."}
	}
	if obj == nil {
		return "", invalid(p)
	}
	// Source OpenAI запрещает неизвестные поля; OpenRouter исторически их игнорирует.
	if p == aihttp.OpenAI {
		for k := range obj {
			if k != "apiKey" {
				return "", invalid(p)
			}
		}
	}
	var key string
	_ = json.Unmarshal(obj["apiKey"], &key)
	key = strings.TrimFunc(key, func(r rune) bool {
		return r == 0xfeff || r == '\u00a0' || r == '\t' || r == '\n' || r == '\v' || r == '\f' || r == '\r' || r == ' ' || r == '\u1680' || r >= '\u2000' && r <= '\u200a' || r == '\u2028' || r == '\u2029' || r == '\u202f' || r == '\u205f' || r == '\u3000'
	})
	if e := ValidateKey(p, key); e != nil {
		return "", e
	}
	return key, nil
}

type Repository struct {
	DB           *sql.DB
	secret       string
	openAIEnvKey string
}

func NewRepository(db *sql.DB, secret, openAIEnvKey string) Repository {
	return Repository{DB: db, secret: secret, openAIEnvKey: openAIEnvKey}
}

func (r Repository) String() string   { return "aiproviders.Repository{credentials:redacted}" }
func (r Repository) GoString() string { return r.String() }

type Settings struct {
	Configured bool    `json:"configured"`
	KeyMask    string  `json:"key_mask"`
	Revision   int64   `json:"revision"`
	UpdatedAt  *string `json:"updated_at"`
}
type stored struct {
	encrypted, suffix string
	revision          int64
	updated           sql.NullString
}

func table(p aihttp.Provider) string {
	if p == aihttp.OpenAI {
		return "openai_settings"
	}
	return "openrouter_settings"
}
func read(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, p aihttp.Provider) (stored, error) {
	var s stored
	query := "SELECT key_encrypted,key_suffix,revision,updated_at FROM openai_settings WHERE id=1"
	if p == aihttp.OpenRouter {
		query = "SELECT key_encrypted,key_suffix,revision,NULL FROM openrouter_settings WHERE id=1"
	}
	err := q.QueryRowContext(ctx, query).Scan(&s.encrypted, &s.suffix, &s.revision, &s.updated)
	if errors.Is(err, sql.ErrNoRows) {
		return stored{}, nil
	}
	if err != nil {
		return stored{}, ErrStorage
	}
	return s, nil
}
func (r Repository) Read(ctx context.Context, p aihttp.Provider) (Settings, error) {
	if r.DB == nil || !validProvider(p) {
		return Settings{}, ErrStorage
	}
	s, e := read(ctx, r.DB, p)
	if e != nil {
		return Settings{}, e
	}
	out := Settings{Configured: s.encrypted != "" || p == aihttp.OpenAI && r.openAIEnvKey != "", Revision: s.revision}
	if s.suffix != "" {
		out.KeyMask = "••••" + s.suffix
	}
	if s.updated.Valid {
		out.UpdatedAt = &s.updated.String
	}
	return out, nil
}
func (r Repository) Resolve(ctx context.Context, p aihttp.Provider) (string, error) {
	if r.DB == nil || !validProvider(p) {
		return "", ErrStorage
	}
	s, e := read(ctx, r.DB, p)
	if e != nil {
		return "", e
	}
	if s.encrypted != "" {
		return r.decryptStored(p, s.encrypted)
	}
	if p == aihttp.OpenAI && strings.TrimSpace(r.openAIEnvKey) != "" {
		return strings.TrimSpace(r.openAIEnvKey), nil
	}
	return "", ErrMissing
}
func (r Repository) Generation(ctx context.Context) (string, error) {
	if r.DB == nil {
		return "", ErrStorage
	}
	var g string
	if e := r.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&g); e != nil || utf8.RuneCountInString(g) != 36 {
		return "", ErrStorage
	}
	return g, nil
}
func (r Repository) SaveKey(ctx context.Context, p aihttp.Provider, generation, key string) error {
	if r.DB == nil || !validProvider(p) {
		return ErrStorage
	}
	if e := ValidateKey(p, key); e != nil {
		return e
	}
	codec, e := legacycrypto.New(r.secret, legacycrypto.Domain(":"+string(p)+":v1"))
	if e != nil {
		return ErrStorage
	}
	encrypted, e := codec.Encrypt(key)
	if e != nil {
		return ErrStorage
	}
	tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return ErrStorage
	}
	defer tx.Rollback()
	var actual string
	if e = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&actual); e != nil {
		return ErrStorage
	}
	if actual != generation || utf8.RuneCountInString(actual) != 36 {
		return ErrGeneration
	}
	// Параллельные сохранения сериализуются PK singleton; все изменения одной TX.
	if _, e = tx.ExecContext(ctx, "INSERT INTO "+table(p)+"(id) VALUES(1) ON DUPLICATE KEY UPDATE id=VALUES(id)"); e != nil {
		return ErrStorage
	}
	query := "UPDATE openai_settings SET key_encrypted=?,key_suffix=?,revision=revision+1,updated_at=UTC_TIMESTAMP() WHERE id=1"
	if p == aihttp.OpenRouter {
		query = "UPDATE openrouter_settings SET key_encrypted=?,key_suffix=?,connection_status='unchecked',last_error='',revision=revision+1 WHERE id=1"
	}
	if _, e = tx.ExecContext(ctx, query, encrypted, key[len(key)-4:]); e != nil {
		return ErrStorage
	}
	if e = tx.Commit(); e != nil {
		return ErrStorage
	}
	return nil
}

func (r Repository) decryptStored(p aihttp.Provider, encrypted string) (string, error) {
	codec, e := legacycrypto.New(r.secret, legacycrypto.Domain(":"+string(p)+":v1"))
	if e != nil {
		return "", ErrDecrypt
	}
	key, e := codec.Decrypt(encrypted)
	if e != nil {
		return "", ErrDecrypt
	}
	return key, nil
}
