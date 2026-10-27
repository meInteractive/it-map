// Package channels reads and mutates independent Telegram sources.
package channels

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
)

type Channel struct {
	ID                    int64   `json:"id"`
	Name                  string  `json:"name"`
	URL                   string  `json:"url"`
	Username              string  `json:"username"`
	NormalizedUsername    string  `json:"normalizedUsername"`
	LastSuccessfulCheckAt *string `json:"lastSuccessfulCheckAt"`
	Comment               string  `json:"comment"`
	VacancyCount          int64   `json:"vacancyCount"`
	CreatedAt             string  `json:"createdAt"`
	UpdatedAt             string  `json:"updatedAt"`
}

type Repository struct {
	DB *sql.DB
	// CatalogSources normalizes an unordered URL projection in memory. The
	// composition root supplies the accepted source-compatible field policy;
	// channel writes do not depend on catalog rank or locale ordering.
	CatalogSources func([]byte) ([]string, error)
}

type channelQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (r Repository) List(ctx context.Context) ([]Channel, error) {
	if r.DB == nil {
		return nil, errors.New("channels database is not configured")
	}
	return listChannels(ctx, r.DB)
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, v := range b {
		if v >= 'A' && v <= 'Z' {
			b[i] = v + ('a' - 'A')
		}
	}
	return string(b)
}

func sourceChannelOrder(a, b Channel) bool {
	key := func(s string) string {
		if i := strings.IndexByte(s, 0); i >= 0 {
			s = s[:i]
		}
		return asciiLower(s)
	}
	ak, bk := key(a.Name), key(b.Name)
	if ak != bk {
		return ak < bk
	}
	// SQLite NOCASE stops at embedded NUL then compares byte lengths. The
	// source GROUP BY emits IDs in key order before equal-name sorting.
	if len(a.Name) != len(b.Name) {
		return len(a.Name) < len(b.Name)
	}
	return a.ID < b.ID
}

func listChannels(ctx context.Context, q channelQueryer) ([]Channel, error) {
	// Pre-aggregate counts to avoid depending on permissive GROUP BY semantics.
	// No writes are performed; an active vacancy may appear in more than one
	// channel just as in the v316 query.
	const query = `SELECT c.id,c.name,c.url,c.username,c.normalized_username,
 c.last_successful_check_at,c.comment,c.created_at,c.updated_at,
 COALESCE(v.cnt,0) FROM telegram_channels AS c
 LEFT JOIN (SELECT channel_id,COUNT(vacancy_id) AS cnt
            FROM vacancy_telegram_sources GROUP BY channel_id) AS v ON v.channel_id=c.id
 ORDER BY c.id`
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Channel, 0)
	for rows.Next() {
		var ch Channel
		var username, checked sql.NullString
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.URL, &username, &ch.NormalizedUsername, &checked, &ch.Comment, &ch.CreatedAt, &ch.UpdatedAt, &ch.VacancyCount); err != nil {
			return nil, err
		}
		if username.Valid && username.String != "" {
			ch.Username = username.String
		} else {
			ch.Username = "@" + ch.NormalizedUsername
		}
		if checked.Valid && checked.String != "" {
			v := checked.String
			ch.LastSuccessfulCheckAt = &v
		}
		result = append(result, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(result, func(i, j int) bool { return sourceChannelOrder(result[i], result[j]) })
	return result, nil
}
