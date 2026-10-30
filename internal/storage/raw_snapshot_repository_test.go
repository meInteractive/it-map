//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"itmap/internal/parser"
	"os/exec"
	"strings"
	"testing"
)

func TestMariaDBRawSnapshotSourceDisposable(t *testing.T) {
	f := billingFixture(t)
	cmd := exec.Command("node", "../../scripts/check-current-snapshot.mjs")
	raw, e := cmd.Output()
	if e != nil {
		t.Fatal("source snapshot oracle", e)
	}
	var frames []struct {
		Name, Source string
		Rows         []struct {
			ID, Created, Status, Hash string
			Raw                       *string
			Parts                     []string
		}
		Result *struct{ ID, Markdown, Hash, Created string }
	}
	if json.Unmarshal(raw, &frames) != nil || len(frames) != 7 {
		t.Fatal("source corpus")
	}
	if f.count("SELECT COUNT(*) FROM vacancy_candidates WHERE id=990001") != 0 {
		t.Fatal("synthetic candidate collision")
	}
	f.exec("INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at)VALUES(990001,'CI_TOKEN','CI_TITLE',?,'manual','CI_TIME','CI_TIME','CI_TIME','CI_TIME')", strings.Repeat("a", 64))
	t.Cleanup(func() { f.exec("DELETE FROM vacancy_candidates WHERE id=990001") })
	read := func(source string) (parser.StoredRawSnapshot, error) {
		tx, e := f.db.BeginTx(f.ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		return parser.ReadRawSnapshot(f.ctx, tx, 990001, "https://example.com/ci", source)
	}
	for _, frame := range frames {
		t.Run(frame.Name, func(t *testing.T) {
			f.exec("DELETE FROM vacancy_raw_snapshots WHERE candidate_id=990001")
			for _, r := range frame.Rows {
				f.exec("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at) VALUES(?,990001,'https://example.com/ci','https://example.com/ci',?,?,'CI','free',?,'{}',?)", r.ID, r.Raw, r.Hash, r.Status, r.Created)
				for i, part := range r.Parts {
					f.exec("INSERT INTO vacancy_raw_snapshot_parts(snapshot_id,part_index,markdown_raw)VALUES(?,?,?)", r.ID, i, part)
				}
			}
			source := frame.Source
			if source == "" {
				source = "companySite"
			}
			v, e := read(source)
			if frame.Result == nil {
				if !errors.Is(e, parser.ErrSnapshotMissing) {
					t.Fatal("source missing result differs", e)
				}
				return
			}
			if e != nil || v.ID != frame.Result.ID || v.ContentHash != frame.Result.Hash || v.Markdown != frame.Result.Markdown || v.CreatedAt != frame.Result.Created {
				t.Fatal("source selected snapshot differs", e)
			}
		})
	}
	t.Run("corrupt_hash", func(t *testing.T) {
		f.exec("DELETE FROM vacancy_raw_snapshots WHERE candidate_id=990001")
		f.exec("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at) VALUES('CI_BAD',990001,'https://example.com/ci','https://example.com/ci','CI_BODY',?,'CI','free','valid','{}','2026-01-01')", strings.Repeat("a", 64))
		if _, e := read("companySite"); !errors.Is(e, parser.ErrSnapshotInvalid) {
			t.Fatal("corrupt hash accepted", e)
		}
	})
	t.Run("ambiguous_latest", func(t *testing.T) {
		f.exec("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at) VALUES('CI_TIE',990001,'https://example.com/ci','https://example.com/ci','CI_BODY',?,'CI','free','valid','{}','2026-01-01')", strings.Repeat("a", 64))
		if _, e := read("companySite"); !errors.Is(e, parser.ErrSnapshotAmbiguous) {
			t.Fatal("ambiguous snapshot guessed", e)
		}
	})
	t.Run("part_gap", func(t *testing.T) {
		f.exec("DELETE FROM vacancy_raw_snapshots WHERE candidate_id=990001")
		f.exec("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at) VALUES('CI_PART',990001,'https://example.com/ci','https://example.com/ci',NULL,?,'CI','free','valid','{}','2026-01-01')", strings.Repeat("a", 64))
		f.exec("INSERT INTO vacancy_raw_snapshot_parts(snapshot_id,part_index,markdown_raw)VALUES('CI_PART',1,'CI_BODY')")
		if _, e := read("companySite"); !errors.Is(e, parser.ErrSnapshotInvalid) {
			t.Fatal("missing part accepted", e)
		}
	})
}
