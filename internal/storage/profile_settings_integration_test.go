//go:build mariadb && profilewrites

package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/profiles"
)

type promptStorageProjection struct {
	Kind, TextHash string
	Revision       int64
	HasUpdatedAt   bool
}
type modelStorageProjection struct {
	Kind, Provider, OpenAIModel, OpenRouterModel, ReasoningEffort string
	Revision                                                      int64
	HasUpdatedAt                                                  bool
}

func promptProjection(p profiles.Prompt) promptStorageProjection {
	hash := sha256.Sum256([]byte(p.Markdown))
	return promptStorageProjection{Kind: p.Kind, TextHash: hex.EncodeToString(hash[:]), Revision: p.Revision, HasUpdatedAt: p.UpdatedAt != nil}
}
func modelProjection(m profiles.Model) modelStorageProjection {
	return modelStorageProjection{Kind: m.Kind, Provider: m.Provider, OpenAIModel: m.OpenAIModel, OpenRouterModel: m.OpenRouterModel, ReasoningEffort: m.ReasoningEffort, Revision: m.Revision, HasUpdatedAt: m.UpdatedAt != nil}
}
func TestMariaDBProfileSettingsDisposable(t *testing.T) {
	t.Run("snapshot defaults read without initialization or hidden writes", func(t *testing.T) {
		f := openProfileSettingsFixture(t)
		fault, db := openMutationFaultPool(t, "")
		repo := profiles.Repository{DB: db}
		prompts, err := repo.ReadPrompts(f.ctx)
		if err != nil || !reflect.DeepEqual(prompts, profiles.DefaultPrompts()) {
			t.Fatal("shared prompt defaults differ from legacy")
		}
		models, err := repo.ReadModels(f.ctx)
		if err != nil || !reflect.DeepEqual(models, profiles.DefaultModels()) {
			t.Fatal("shared model defaults differ from legacy")
		}
		fault.mu.Lock()
		writes := fault.writes
		fault.mu.Unlock()
		if writes != 0 {
			t.Fatal("settings GET executed a write/DDL statement")
		}
		f.assertEmpty()
	})
	t.Run("actual legacy SQLite storage transitions match MariaDB", func(t *testing.T) {
		path := filepath.Join("..", "profiles", "testdata", "storage-contract.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var fixture struct {
			Vectors []struct {
				Kind, First string
				Steps       []struct {
					Part   string
					Input  json.RawMessage
					Prompt promptStorageProjection
					Model  modelStorageProjection
				}
			}
		}
		if err = json.Unmarshal(data, &fixture); err != nil || len(fixture.Vectors) != 8 {
			t.Fatal("source SQLite transitions missing")
		}
		for _, v := range fixture.Vectors {
			t.Run(v.Kind+"/"+v.First, func(t *testing.T) {
				f := openProfileSettingsFixture(t)
				for _, step := range v.Steps {
					if step.Part == "prompt" {
						in, err := profiles.ParsePrompt(v.Kind, step.Input, 0)
						if err != nil {
							t.Fatal(err)
						}
						f.savePrompt(v.Kind, in.Text, in.Revision)
					} else {
						in, err := profiles.ParseModel(v.Kind, step.Input, 0)
						if err != nil {
							t.Fatal(err)
						}
						f.saveModel(v.Kind, in)
					}
					if !reflect.DeepEqual(promptProjection(f.prompts().Prompts[v.Kind]), step.Prompt) || !reflect.DeepEqual(modelProjection(f.models()[v.Kind]), step.Model) {
						t.Fatal("MariaDB settings transition differs from executed source SQLite semantics")
					}
				}
			})
		}
	})
	t.Run("prompt code-point limits clear stale revisions and model preservation", func(t *testing.T) {
		for _, kind := range []string{"cover_letter", "relevance", "vacancy_extraction", "vacancy_role"} {
			t.Run(kind, func(t *testing.T) {
				f := openProfileSettingsFixture(t)
				text := strings.Repeat("😀", profiles.PromptLimit(kind))
				saved := f.savePrompt(kind, text, 0)
				before := f.prompts()
				model := f.models()[kind]
				if saved.Markdown != text || saved.Revision != 1 || saved.MaxLength != profiles.PromptLimit(kind) || saved.UpdatedAt == nil || before.Prompts[kind].Markdown != text {
					t.Fatal("shared text save lost code points/revision/limit")
				}
				if _, err := f.repo.SavePrompt(f.ctx, kind, profiles.TextInput{Text: "CI stale", Revision: 0}); !errors.Is(err, profiles.ErrConflict) {
					t.Fatal("stale shared prompt accepted")
				}
				if _, err := f.repo.SavePrompt(f.ctx, kind, profiles.TextInput{Text: text + "x", Revision: 1}); profiles.ErrorStatus(err) != 400 {
					t.Fatal("shared prompt code-point limit not enforced")
				}
				cleared := f.savePrompt(kind, "", 1)
				if cleared.Revision != 2 || cleared.Markdown != "" || f.prompts().Prompts[kind].Markdown != "" {
					t.Fatal("cleared prompt was replaced by factory text")
				}
				afterModel := f.models()[kind]
				model.UpdatedAt = nil
				afterModel.UpdatedAt = nil
				if !reflect.DeepEqual(model, afterModel) {
					t.Fatal("text edit changed model fields or its independent revision")
				}
			})
		}
	})
	t.Run("absent stale revisions and invalid model choices leave no rows", func(t *testing.T) {
		f := openProfileSettingsFixture(t)
		for _, kind := range []string{"cover_letter", "relevance", "vacancy_extraction", "vacancy_role"} {
			if _, err := f.repo.SavePrompt(f.ctx, kind, profiles.TextInput{Text: "CI stale", Revision: 1}); !errors.Is(err, profiles.ErrConflict) {
				t.Fatal("stale absent prompt accepted")
			}
			in := freeProfileModel(1)
			if _, err := f.repo.SaveModel(f.ctx, kind, in); !errors.Is(err, profiles.ErrConflict) {
				t.Fatal("stale absent model accepted")
			}
		}
		bad := freeProfileModel(0)
		bad.OpenRouterModel = "jev"
		for _, kind := range []string{"cover_letter", "vacancy_extraction"} {
			if _, err := f.repo.SaveModel(f.ctx, kind, bad); profiles.ErrorStatus(err) != 400 {
				t.Fatal("JEV kind restriction not enforced before SQL")
			}
		}
		f.assertEmpty()
	})
	t.Run("late singleton bootstrap and state faults rollback both revisions", func(t *testing.T) {
		cases := []struct {
			kind, part, prefix string
			existing           bool
		}{
			{"cover_letter", "prompt", "INSERT INTO system_prompts", false},
			{"relevance", "model", "INSERT INTO prompt_model_settings", false},
			{"vacancy_extraction", "prompt", "INSERT INTO vacancy_detail_settings", false},
			{"vacancy_role", "model", "INSERT INTO vacancy_role_settings", false},
			{"cover_letter", "prompt", "UPDATE system_prompts SET", true},
			{"relevance", "model", "UPDATE prompt_model_settings SET", true},
			{"vacancy_extraction", "prompt", "UPDATE vacancy_detail_settings SET", true},
			{"vacancy_role", "model", "UPDATE vacancy_role_settings SET", true},
		}
		for _, c := range cases {
			t.Run(c.kind+"/"+c.part+"/"+c.prefix, func(t *testing.T) {
				f := openProfileSettingsFixture(t)
				var revision int64
				if c.existing {
					f.savePrompt(c.kind, "CI existing text", 0)
					f.saveModel(c.kind, freeProfileModel(0))
					revision = 1
				}
				beforePrompts, beforeModels := f.prompts(), f.models()
				fault, db := openMutationFaultPool(t, c.prefix)
				repo := profiles.Repository{DB: db}
				var err error
				if c.part == "prompt" {
					_, err = repo.SavePrompt(f.ctx, c.kind, profiles.TextInput{Text: "CI interrupted prompt", Revision: revision})
				} else {
					_, err = repo.SaveModel(f.ctx, c.kind, freeProfileModel(revision))
				}
				fault.mu.Lock()
				fired, writes, commits := fault.fired, fault.writes, fault.commits
				fault.mu.Unlock()
				if !errors.Is(err, errMutationFault) || !fired || writes != 1 || commits != 0 {
					t.Fatal("settings fault did not fire after actual SQL before commit")
				}
				if !reflect.DeepEqual(beforePrompts, f.prompts()) || !reflect.DeepEqual(beforeModels, f.models()) {
					t.Fatal("interrupted shared settings left fields/revisions")
				}
				if !c.existing {
					f.assertEmpty()
				}
				db.Close()
				_, fresh := openDisposableMariaDB(t)
				retry := profiles.Repository{DB: fresh}
				if c.part == "prompt" {
					_, err = retry.SavePrompt(f.ctx, c.kind, profiles.TextInput{Text: "CI retry text", Revision: revision})
				} else {
					_, err = retry.SaveModel(f.ctx, c.kind, freeProfileModel(revision))
				}
				if err != nil {
					t.Fatalf("settings fresh-pool retry failed: %T", err)
				}
			})
		}
	})
	t.Run("independent text and model writes and same-revision CAS races", func(t *testing.T) {
		for _, mode := range []string{"prompt", "model", "independent"} {
			t.Run(mode, func(t *testing.T) { testConcurrentProfileSettings(t, mode) })
		}
	})
	t.Run("all five v39 tables preserve ordered columns and key roles", func(t *testing.T) {
		f := openProfileSettingsFixture(t)
		for _, table := range profiles.Schema().Tables {
			rows, err := f.db.QueryContext(f.ctx, `SELECT COLUMN_NAME,DATA_TYPE,IS_NULLABLE,COLUMN_KEY FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? ORDER BY ORDINAL_POSITION`, table.Name)
			if err != nil {
				t.Fatal("cannot inspect settings schema")
			}
			index := 0
			for rows.Next() {
				var name, kind, nullable, key string
				if err = rows.Scan(&name, &kind, &nullable, &key); err != nil {
					rows.Close()
					t.Fatal("invalid settings metadata row")
				}
				if index >= len(table.Columns) {
					rows.Close()
					t.Fatal("extra profile column outside v39")
				}
				want := table.Columns[index]
				index++
				if name != want.Name || (want.Type == "INTEGER" && kind != "bigint") || (want.Type == "TEXT" && kind != "varchar" && kind != "longtext") || (key == "PRI") != want.PrimaryKey || (want.NotNull && nullable != "NO") {
					rows.Close()
					t.Fatal("profile schema lost source columns/types/key/nullability")
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil || index != len(table.Columns) {
				t.Fatal("v39 profile schema is incomplete")
			}
		}
		for _, query := range []string{`INSERT INTO system_prompts(kind) VALUES('CI_unknown')`, `INSERT INTO prompt_model_settings(kind,openrouter_model) VALUES('cover_letter','jev')`, `INSERT INTO vacancy_role_settings(id,markdown) VALUES(2,'CI')`} {
			_, err := f.db.ExecContext(f.ctx, query)
			var sqlErr *mysql.MySQLError
			if !errors.As(err, &sqlErr) || sqlErr.Number != 4025 {
				t.Fatal("required shared settings CHECK not enforced")
			}
		}
	})
}
func testConcurrentProfileSettings(t *testing.T, mode string) {
	f := openProfileSettingsFixture(t)
	f.savePrompt("vacancy_role", "CI initial text", 0)
	f.saveModel("vacancy_role", freeProfileModel(0))
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	holder, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("cannot start settings barrier")
	}
	var id int
	if err = holder.QueryRowContext(ctx, `SELECT id FROM vacancy_role_settings WHERE id=1 FOR UPDATE`).Scan(&id); err != nil {
		holder.Rollback()
		t.Fatal("cannot hold settings row")
	}
	var repos [2]profiles.Repository
	var ids [2]int64
	for i := range repos {
		_, db := openDisposableMariaDB(t)
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		if err = db.QueryRowContext(ctx, `SELECT CONNECTION_ID()`).Scan(&ids[i]); err != nil {
			holder.Rollback()
			t.Fatal("cannot identify settings writer")
		}
		repos[i] = profiles.Repository{DB: db}
	}
	if ids[0] == ids[1] {
		holder.Rollback()
		t.Fatal("settings race requires independent SQL connections")
	}
	var wg sync.WaitGroup
	var outcomes [2]error
	defer func() { cancel(); holder.Rollback(); wg.Wait() }()
	for i := range repos {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if mode == "model" || mode == "independent" && i == 1 {
				in := freeProfileModel(1)
				if i == 0 {
					in.ReasoningEffort = "low"
				} else {
					in.ReasoningEffort = "max"
				}
				_, outcomes[i] = repos[i].SaveModel(ctx, "vacancy_role", in)
			} else {
				text := "CI editor A"
				if i == 1 {
					text = "CI editor B"
				}
				_, outcomes[i] = repos[i].SavePrompt(ctx, "vacancy_role", profiles.TextInput{Text: text, Revision: 1})
			}
		}(i)
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting int
		if err = f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID IN (?,?) AND INFO LIKE 'INSERT INTO vacancy_role_settings%'`, ids[0], ids[1]).Scan(&waiting); err != nil {
			t.Fatal("cannot observe both settings writers")
		}
		if waiting == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("both settings writers never reached SQL barrier")
		case <-tick.C:
		}
	}
	if err = holder.Rollback(); err != nil {
		t.Fatal("cannot release settings barrier")
	}
	wg.Wait()
	success, conflict := 0, 0
	for _, err := range outcomes {
		if err == nil {
			success++
		} else if errors.Is(err, profiles.ErrConflict) {
			conflict++
		} else {
			t.Fatalf("unexpected settings race outcome: %T", err)
		}
	}
	prompt, model := f.prompts().Prompts["vacancy_role"], f.models()["vacancy_role"]
	if mode == "independent" {
		if success != 2 || conflict != 0 || prompt.Revision != 2 || model.Revision != 2 || prompt.Markdown != "CI editor A" || model.ReasoningEffort != "max" {
			t.Fatal("independent prompt/model revisions lost a write")
		}
	} else {
		if success != 1 || conflict != 1 {
			t.Fatal("one revision admitted two settings writers")
		}
		if mode == "prompt" && (prompt.Revision != 2 || model.Revision != 1) || mode == "model" && (model.Revision != 2 || prompt.Revision != 1) {
			t.Fatal("settings CAS changed the independent revision")
		}
	}
}
