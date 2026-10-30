package parser

import (
	"context"
	"database/sql"
	"errors"
)

var ErrSnapshotMissing = errors.New("valid vacancy raw snapshot not found")
var ErrSnapshotAmbiguous = errors.New("latest vacancy raw snapshot is ambiguous")

type StoredRawSnapshot struct{ ID, ContentHash, SourceURL, FinalURL, CreatedAt, Markdown string }
type snapshotRow struct {
	StoredRawSnapshot
	inline sql.NullString
	status string
}

// Читает исходный снимок в транзакции вызывающей стадии. Владелец, scope и
// текущий URL кандидата должны быть проверены в той же owned транзакции.
func ReadRawSnapshot(ctx context.Context, tx *sql.Tx, candidateID int64, sourceURL, sourceID string) (StoredRawSnapshot, error) {
	if tx == nil || candidateID < 1 || sourceURL == "" || sourceID == "" {
		return StoredRawSnapshot{}, ErrJobInput
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,content_hash,source_url,final_url,created_at,markdown_raw,content_status FROM vacancy_raw_snapshots WHERE candidate_id=? AND source_url=? ORDER BY created_at DESC,id DESC LIMIT 2 FOR UPDATE`, candidateID, sourceURL)
	if e != nil {
		return StoredRawSnapshot{}, ErrSettingsStorage
	}
	found := []snapshotRow{}
	for rows.Next() {
		var r snapshotRow
		if e = rows.Scan(&r.ID, &r.ContentHash, &r.SourceURL, &r.FinalURL, &r.CreatedAt, &r.inline, &r.status); e != nil {
			break
		}
		found = append(found, r)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return StoredRawSnapshot{}, ErrSettingsStorage
	}
	if len(found) == 0 {
		return StoredRawSnapshot{}, ErrSnapshotMissing
	}
	if len(found) > 1 && found[0].CreatedAt == found[1].CreatedAt {
		return StoredRawSnapshot{}, ErrSnapshotAmbiguous
	}
	s := found[0]
	if s.status != "valid" {
		return StoredRawSnapshot{}, ErrSnapshotMissing
	}
	var inline *string
	parts := []RawSnapshotPart{}
	if s.inline.Valid {
		inline = &s.inline.String
	} else {
		rows, e = tx.QueryContext(ctx, "SELECT part_index,markdown_raw FROM vacancy_raw_snapshot_parts WHERE snapshot_id=? ORDER BY part_index FOR UPDATE", s.ID)
		if e != nil {
			return StoredRawSnapshot{}, ErrSettingsStorage
		}
		for rows.Next() {
			var part RawSnapshotPart
			if e = rows.Scan(&part.Index, &part.Markdown); e != nil {
				break
			}
			parts = append(parts, part)
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return StoredRawSnapshot{}, ErrSettingsStorage
		}
	}
	s.Markdown, e = AssembleRawSnapshot(inline, parts, s.ContentHash)
	if e != nil {
		return StoredRawSnapshot{}, e
	}
	if sourceID == "rabota" && !RabotaBodyPresent(s.Markdown) {
		return StoredRawSnapshot{}, ErrSnapshotMissing
	}
	return s.StoredRawSnapshot, nil
}
