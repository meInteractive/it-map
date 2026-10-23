// Package channels projects independent Telegram sources without writes.
// Channel creation/update/delete remains disabled until administrative APIs
// and the migration of the database's uniqueness constraints are complete.
package channels

import (
	"context"
	"database/sql"
	"errors"
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

type Repository struct{ DB *sql.DB }

func (r Repository) List(ctx context.Context) ([]Channel, error) {
	if r.DB == nil {
		return nil, errors.New("channels database is not configured")
	}
	// Pre-aggregate counts to avoid depending on permissive GROUP BY semantics.
	// No writes are performed; an active vacancy may appear in more than one
	// channel just as in the v316 query.
	const query = `SELECT c.id,c.name,c.url,c.username,c.normalized_username,
 c.last_successful_check_at,c.comment,c.created_at,c.updated_at,
 COALESCE(v.cnt,0) FROM telegram_channels AS c
 LEFT JOIN (SELECT channel_id,COUNT(vacancy_id) AS cnt
            FROM vacancy_telegram_sources GROUP BY channel_id) AS v ON v.channel_id=c.id
 ORDER BY LOWER(c.name) ASC,c.name ASC,c.id ASC`
	rows, err := r.DB.QueryContext(ctx, query)
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
	return result, nil
}
