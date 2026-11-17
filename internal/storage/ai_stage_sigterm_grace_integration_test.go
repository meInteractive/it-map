//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"itmap/internal/parser"
	"itmap/internal/workerloop"
)

// Проверяем настоящий SIGTERM после подтверждённого ответа AI-провайдера и
// durable settlement, но до атомарной записи результата и стоимости.
// Дочерний процесс работает только с одноразовой MariaDB и synthetic provider.
func TestMariaDBAIStageSIGTERMGraceAfterACKDisposable(t *testing.T) {
	const (
		marker = "CI_AI_ACK_SETTLED_BEFORE_STAGE_SAVE"
		jobID  = "CI_AI_SIGTERM_JOB"
	)
	if os.Getenv("ITMAP_AI_SIGTERM_CHILD") == "1" {
		ctx, db := schedulerDB(t)
		defer db.Close()
		root, stop := signal.NotifyContext(ctx, syscall.SIGTERM)
		defer stop()
		repo := parser.RunRepository{DB: db, Generation: os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION")}
		stage := &parser.AIStageExecutor{
			Store: &aiStageFixtureStore{},
			Generator: &aiStageFixtureRunner{afterBilling: func() {
				fmt.Println(marker)
				// Не продолжаем до реального сигнала; grace сохраняет контекст шага.
				<-root.Done()
			}},
		}
		profile := workerloop.Profile{
			Name: workerloop.Pipeline, WorkDelay: time.Second, IdleDelay: time.Second,
			ErrorMin: time.Second, ErrorMax: 2 * time.Second, StepTimeout: 20 * time.Second,
			Step: func(stepCtx context.Context) (bool, error) {
				run, err := repo.Begin(stepCtx, parser.RunOptions{RequestID: "CI_AI_SIGTERM_RUN"})
				if err != nil || !run.Acquired {
					return false, errors.Join(errors.New("synthetic run not acquired"), err)
				}
				lease, err := repo.ClaimJob(stepCtx, run.Run.ID, parser.ClaimOptions{JobID: func() *string { id := jobID; return &id }()})
				if err != nil || lease == nil {
					return false, errors.Join(errors.New("synthetic job not claimed"), err)
				}
				if _, err := stage.Execute(stepCtx, lease); err != nil {
					return false, err
				}
				return true, repo.Finish(stepCtx, run.Run.ID, "completed")
			},
		}
		if err := workerloop.RunProfilesWithGrace(root, []workerloop.Profile{profile}, nil, 8*time.Second); !errors.Is(err, context.Canceled) {
			t.Fatal("graceful SIGTERM did not return cancellation after the settled AI job", err)
		}
		return
	}

	f := billingFixture(t)
	billingReset(f)
	if f.count("SELECT COUNT(*) FROM vacancy_candidates WHERE id=990001") != 0 ||
		f.count("SELECT COUNT(*) FROM vacancy_classification_attempts WHERE candidate_id=990001") != 0 {
		t.Fatal("synthetic candidate namespace not empty")
	}
	f.exec("INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(990001,'CI_SIGTERM_CANDIDATE','CI_TITLE',?,'manual','CI_TIME','CI_TIME','CI_TIME','CI_TIME')", strings.Repeat("a", 64))
	t.Cleanup(func() {
		f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
		f.exec("DELETE FROM vacancy_candidates WHERE id=990001")
	})
	schedulerSeedJob(f, jobID, nil)
	f.exec("UPDATE scheduler_jobs SET type='extract_vacancy' WHERE id=?", jobID)

	cmd := exec.Command(os.Args[0], "-test.run=^TestMariaDBAIStageSIGTERMGraceAfterACKDisposable$", "-test.count=1")
	cmd.Env = append(os.Environ(), "ITMAP_AI_SIGTERM_CHILD=1")
	pipe, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		t.Fatal("cannot start synthetic AI SIGTERM child", err)
	}
	reaped := false
	defer func() {
		if !reaped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			if scanner.Text() == marker {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("child did not reach settled AI response before Save")
		}
	case <-time.After(12 * time.Second):
		t.Fatal("synthetic AI settlement did not finish")
	}
	// Между settlement и stage Save нет удерживаемой SQL-транзакции.
	if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND status='running' AND reserved_usd BETWEEN 0.0009 AND 0.0011 AND cost_usd IS NULL", jobID) != 1 ||
		f.count("SELECT COUNT(*) FROM vacancy_classification_attempts WHERE candidate_id=990001") != 0 {
		t.Fatal("synthetic provider response was not settled before SIGTERM")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal("could not send graceful SIGTERM", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal("child failed to finish AI result and ledger within grace", err)
	}
	reaped = true
	if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND status='completed' AND cost_usd BETWEEN 0.0009 AND 0.0011", jobID) != 1 ||
		f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=?", jobID) != 1 ||
		f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id=? AND status='completed'", jobID) != 1 ||
		f.count("SELECT COUNT(*) FROM scheduler_runs WHERE status='completed'") != 1 ||
		f.count("SELECT COUNT(*) FROM vacancy_classification_attempts WHERE id=?", "CI_STAGE_"+jobID) != 1 {
		t.Fatal("graceful SIGTERM lost result, paid receipt or owner completion")
	}
	// Повторный claim уже завершённого задания не создаёт платный запрос.
	next := f.begin("CI_AI_SIGTERM_NO_REPLAY")
	id := jobID
	lease, err := f.repo.ClaimJob(f.ctx, next.Run.ID, parser.ClaimOptions{JobID: &id})
	if err != nil || lease != nil || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=?", jobID) != 1 {
		t.Fatal("finished AI job was reclaimed after SIGTERM", err)
	}
	f.finish(next)
}
