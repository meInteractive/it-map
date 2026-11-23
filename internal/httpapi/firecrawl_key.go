package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"itmap/internal/auth"
	"itmap/internal/legacycrypto"
)

var firecrawlKeyPattern = regexp.MustCompile(`^fc-[A-Za-z0-9_-]{12,512}$`)

func firecrawlKeyWrite(manager *auth.Manager, db *sql.DB, codec *legacycrypto.Codec) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(manager, r)
		if !ok || !user.IsAdmin || !writeSameOrigin(r) {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Недостаточно прав для обновления ключа Firecrawl."})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		var input struct {
			Key      string `json:"apiKey"`
			Revision *int64 `json:"revision"`
			Mode     string `json:"mode"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		d.DisallowUnknownFields()
		err := d.Decode(&input)
		key := strings.TrimSpace(input.Key)
		if err != nil || d.Decode(new(any)) != io.EOF || !firecrawlKeyPattern.MatchString(key) ||
			input.Revision == nil || *input.Revision < 0 || *input.Revision >= 9007199254740991 ||
			(input.Mode != "free" && input.Mode != "hobby") {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Укажите ключ Firecrawl, профиль и текущую версию настроек."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		storageError := func() {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Не удалось сохранить ключ Firecrawl."})
		}
		if db == nil || codec == nil {
			storageError()
			return
		}
		encrypted, err := codec.Encrypt(key)
		if err != nil {
			storageError()
			return
		}
		digest := sha256.Sum256([]byte(key))
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			storageError()
			return
		}
		defer tx.Rollback()
		var generation string
		if err = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation); err != nil {
			storageError()
			return
		}
		if *input.Revision == 0 {
			if _, err = tx.ExecContext(ctx, "INSERT INTO firecrawl_settings(id) VALUES(1) ON DUPLICATE KEY UPDATE id=id"); err != nil {
				storageError()
				return
			}
		}
		query := `UPDATE firecrawl_settings SET key_encrypted=?,key_suffix=?,free_key_hash=?,connection_status='unchecked',
			last_error='',checked_at=NULL,free_usage_json='',revision=revision+1 WHERE id=1 AND revision=?`
		if input.Mode == "hobby" {
			// As in the legacy API, changing a Hobby key requires revalidating the paid profile.
			query = `UPDATE firecrawl_settings SET hobby_key_encrypted=?,hobby_key_suffix=?,hobby_key_hash=?,
				hobby_connection_status='unchecked',hobby_last_error='',hobby_checked_at=NULL,hobby_usage_json='',
				mode='free',proxy='basic',enhanced_allowed=0,enabled=CASE WHEN key_encrypted<>'' THEN enabled ELSE 0 END,
				revision=revision+1 WHERE id=1 AND revision=?`
		}
		result, err := tx.ExecContext(ctx, query, encrypted, key[len(key)-4:], hex.EncodeToString(digest[:]), *input.Revision)
		if err != nil {
			storageError()
			return
		}
		n, err := result.RowsAffected()
		if err != nil {
			storageError()
			return
		}
		if n != 1 {
			jsonReply(w, http.StatusConflict, map[string]string{"error": "Настройки Firecrawl изменились. Обновите страницу и повторите."})
			return
		}
		if tx.Commit() != nil {
			storageError()
			return
		}
		jsonReply(w, http.StatusOK, map[string]bool{"ok": true})
	})
}
