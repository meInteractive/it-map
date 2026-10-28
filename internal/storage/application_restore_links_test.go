//go:build mariadb && vacancycreates && vacancyedits && vacancydeletes && v39restore && restorehttp && applicationlinks

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"itmap/internal/applications"
	"itmap/internal/backup"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMariaDBApplicationRestoreLinkDisposable(t *testing.T) {
	for _, operation := range []string{"replay", "delete"} {
		for _, restoreFirst := range []bool{true, false} {
			t.Run(operation+map[bool]string{true: " restore first", false: " application first"}[restoreFirst], func(t *testing.T) {
				f := openRestoreFixture(t)
				if f.count("SELECT COUNT(*) FROM application_creation_receipts WHERE user_name='Demo'") != 0 {
					t.Fatal("restore-link requires empty synthetic receipt namespace")
				}
				// Register exact owned receipt cleanup before a barrier and before generation
				// rotation. The fault pool guard must verify the original workflow nonce.
				t.Cleanup(func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if _, e := f.db.ExecContext(ctx, "DELETE FROM application_creation_receipts WHERE user_name='Demo' AND idempotency_key=?", appLinkKey); e != nil {
						t.Error("cannot clean restore-link intent")
					}
				})
				prefix := "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE"
				if operation == "delete" {
					if restoreFirst {
						prefix = "DELETE FROM `company_applications`"
					} else {
						prefix = "DELETE FROM company_applications"
					}
				}
				fault, db, release := appLinkBarrier(t, prefix)
				f.restore(f.body)
				if f.count("SELECT COUNT(*) FROM company_applications WHERE user_name IN ('Demo','Nastya')") != 0 || f.count("SELECT COUNT(*) FROM application_creation_receipts WHERE user_name='Demo'") != 0 {
					t.Fatal("restore-link fixture requires unused synthetic users")
				}
				const company = "Synthetic company"
				apps := applications.Repository{DB: f.db, CreationSecret: []byte(strings.Repeat("l", 64))}
				token, e := apps.CreationContext(f.ctx, "Demo")
				if e != nil {
					t.Fatal("restore-link context unavailable")
				}
				vacancyID := int64(1)
				input := applications.CreateInput{CompanyName: company, VacancyID: &vacancyID, Status: "Откликнулся", Comment: "CI restored Demo application", ApplicationSources: []string{"site"}, IdempotencyKey: appLinkKey, CreationContext: token}
				created, e := apps.CreateWithReceipt(f.ctx, "Demo", input)
				if e != nil {
					t.Fatal("cannot seed linked restore intent")
				}
				demoID := created.CreatedApplicationID
				result, e := f.db.ExecContext(f.ctx, "INSERT INTO company_applications(user_name,company_name,vacancy_id,status,comment_md,application_sources)VALUES('Nastya',?,1,'Откликнулся','CI Nastya restore private','[\"site\"]')", company)
				if e != nil {
					t.Fatal("cannot seed another restore owner")
				}
				otherID, e := result.LastInsertId()
				if e != nil {
					t.Fatal("missing synthetic other owner identity")
				}
				f.exec("INSERT INTO company_application_status_history(application_id,from_status,to_status)VALUES(?,'','Откликнулся')", otherID)
				otherBefore, e := apps.ByID(f.ctx, "Nastya", otherID)
				if e != nil {
					t.Fatal("cannot read other restore owner")
				}
				body := f.download()
				payload, e := backup.ValidateV39(body)
				if e != nil {
					t.Fatal("synthetic linked restore export invalid")
				}
				oldGeneration := f.generation()
				workApps := apps
				workApps.DB = db
				restorer := backup.Repository{DB: db}
				restored, application := make(chan appLinkOutcome, 1), make(chan appLinkOutcome, 1)
				restoreStarted, applicationStarted, restoreJoined, applicationJoined := false, false, false, false
				t.Cleanup(func() {
					release()
					if restoreStarted && !restoreJoined {
						select {
						case <-restored:
						case <-time.After(8 * time.Second):
							t.Error("restore actor did not stop before fixture cleanup")
						}
					}
					if applicationStarted && !applicationJoined {
						select {
						case <-application:
						case <-time.After(8 * time.Second):
							t.Error("application actor did not stop before fixture cleanup")
						}
					}
				})

				startRestore := func() { go func() { _, e := restorer.RestoreV39(f.ctx, body); restored <- appLinkOutcome{err: e} }() }
				startApplication := func() {
					applicationStarted = true
					go func() {
						if operation == "replay" {
							out, e := workApps.CreateWithReceipt(f.ctx, "Demo", input)
							application <- appLinkOutcome{created: out, err: e}
						} else {
							_, e := workApps.Delete(f.ctx, "Demo", demoID)
							application <- appLinkOutcome{err: e}
						}
					}()
				}
				if restoreFirst {
					startRestore()
				} else {
					startApplication()
				}
				appLinkEntered(t, fault.entered)
				if restoreFirst {
					startApplication()
					if operation == "replay" {
						appLinkWait(t, f.ctx, f.db, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE")
					} else {
						appLinkWait(t, f.ctx, f.db, "SELECT company_name FROM company_applications")
					}
				} else {
					startRestore()
					if operation == "replay" {
						appLinkWait(t, f.ctx, f.db, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE")
					} else {
						// Counter preflight obtains table WRITE locks before the data TX.
						appLinkWait(t, f.ctx, f.db, "LOCK TABLES `company_application_status_history` WRITE", "LOCK TABLES `company_applications` WRITE", "DELETE FROM `company_application_status_history`", "DELETE FROM `company_applications`")
					}
				}
				release()
				r := appLinkDone(t, restored)
				restoreJoined = true
				a := appLinkDone(t, application)
				applicationJoined = true
				if r.err != nil {
					t.Fatalf("linked synthetic restore failed: %T", r.err)
				}
				if operation == "replay" {
					if restoreFirst && !errors.Is(a.err, applications.ErrCreationContextExpired) {
						t.Fatalf("old linked replay crossed restored generation: %T", a.err)
					}
					if !restoreFirst && a.err != nil && !errors.Is(a.err, applications.ErrCreationContextExpired) {
						t.Fatalf("serialized linked replay failed: %T", a.err)
					}
					if a.err == nil && (!a.created.Replayed || a.created.CreatedApplicationID != demoID) {
						t.Fatal("restore race changed durable linked identity")
					}
				} else if a.err != nil {
					t.Fatalf("serialized application deletion failed: %T", a.err)
				}
				// A delete after restore removes only that user's restored row/history. A
				// restore after delete intentionally reinstates its trusted snapshot.
				if operation == "delete" && restoreFirst {
					for i := range payload.Storage.Tables {
						table := &payload.Storage.Tables[i]
						key := ""
						switch table.Name {
						case "company_applications":
							key = "id"
						case "company_application_status_history":
							key = "application_id"
						}
						if key == "" {
							continue
						}
						kept := table.Rows[:0]
						for _, row := range table.Rows {
							var id int64
							_ = json.Unmarshal(row[key], &id)
							if id != demoID {
								kept = append(kept, row)
							}
						}
						table.Rows = kept
					}
				}
				if !reflect.DeepEqual(f.snapshot(), restoreRecords(t, payload.Storage.Tables)) {
					t.Fatal("restore-link ordering left partial rows or changed unrelated records")
				}
				otherAfter, e := apps.ByID(f.ctx, "Nastya", otherID)
				if e != nil || !reflect.DeepEqual(otherBefore, otherAfter) {
					t.Fatal("restore/delete changed another user's application/history")
				}
				if f.generation() == oldGeneration {
					t.Fatal("linked restore failed to rotate generation")
				}
				if _, e = apps.CreateWithReceipt(f.ctx, "Demo", input); !errors.Is(e, applications.ErrCreationContextExpired) {
					t.Fatal("old linked context survived restore")
				}
				fresh, e := apps.CreationContext(f.ctx, "Demo")
				if e != nil {
					t.Fatal("fresh linked context unavailable")
				}
				input.CreationContext = fresh
				if _, e = apps.CreateWithReceipt(f.ctx, "Demo", input); !errors.Is(e, applications.ErrCreationContextExpired) {
					t.Fatal("old linked receipt was reused in a fresh generation")
				}
				if f.count("SELECT COUNT(*) FROM application_creation_receipts WHERE user_name='Demo'") != 1 {
					t.Fatal("restore erased or duplicated linked receipt tombstone")
				}
			})
		}
	}
}
