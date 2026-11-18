//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"errors"
	"testing"

	"itmap/internal/coverletters"
)

// Вся проверка на одноразовой CI MariaDB; OpenAI заменён mock провайдером.
// Ни ключей, ни HTTP-запросов, ни реальных расходов.
func TestMariaDBCoverLetterPaidAdmissionDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := openSettingsSQL(t)
	count := func(query string, args ...any) int {
		var n int
		if err := f.db.QueryRowContext(f.ctx, query, args...).Scan(&n); err != nil {
			t.Fatal("paid CI count query failed", err)
		}
		return n
	}
	const vacancyID = 190019
	const user = "ci-letter-paid"
	if count("SELECT COUNT(*) FROM company_vacancies WHERE id=?", vacancyID) != 0 {
		t.Fatal("paid letter CI namespace is not empty")
	}
	t.Cleanup(func() {
		for _, query := range []string{
			"DELETE FROM cover_letter_generation_leases WHERE user_name='ci-letter-paid'",
			"DELETE FROM vacancy_cover_letters WHERE user_name='ci-letter-paid'",
			"DELETE FROM openrouter_ai_logs WHERE task='openai_cover_letter' AND requested_primary_model='gpt-6-luna'",
			"DELETE FROM user_profiles WHERE user_name='ci-letter-paid'",
			"DELETE FROM system_prompts WHERE kind='cover_letter'",
			"DELETE FROM prompt_model_settings WHERE kind='cover_letter'",
			"DELETE FROM company_vacancies WHERE id=190019",
			"DELETE FROM company_crm WHERE company_name='CI Paid Letter Employer'",
		} {
			if _, err := f.db.ExecContext(context.Background(), query); err != nil {
				t.Error("paid letter CI cleanup failed", err)
			}
		}
	})
	f.exec("INSERT INTO company_crm(company_name) VALUES('CI Paid Letter Employer')")
	f.exec("INSERT INTO company_vacancies(id,company_name,url,title,description_md) VALUES(190019,'CI Paid Letter Employer','https://example.invalid/cover/19','CI Engineering Manager','CI Requirements')")
	f.exec("INSERT INTO user_profiles(user_name,resume_md,revision) VALUES(?, 'CI Synthetic Resume',1)", user)
	f.exec("INSERT INTO system_prompts(kind,markdown,revision) VALUES('cover_letter','CI Synthetic Prompt',1)")
	f.exec("INSERT INTO prompt_model_settings(kind,provider,openai_model,revision) VALUES('cover_letter','openai','gpt-6-luna',1)")

	mock := &letterMockGenerator{Fail: true, Output: `{"subject":"CI Paid Subject","markdown":"CI Paid body."}`}
	svc := coverletters.Service{Repository: coverletters.Repository{DB: f.db}, Generator: mock}

	// Ответ потерян после принятого synthetic POST: стоимость неизвестна.
	_, err := svc.Generate(f.ctx, user, vacancyID, false, nil)
	if err == nil || mock.Sends != 1 || mock.Calls != 1 {
		t.Fatal("lost provider ACK not reproduced", err, mock.Sends, mock.Calls)
	}
	if count("SELECT COUNT(*) FROM openrouter_ai_logs WHERE task='openai_cover_letter' AND requested_primary_model='gpt-6-luna' AND success=0 AND error_code LIKE 'paid_unknown:%'") != 1 {
		t.Fatal("unknown paid response was not durably retained")
	}
	// Ни исчезнувшая lease, ни новый запрос браузера не разрешают replay.
	mock.Fail = false
	_, err = svc.Generate(f.ctx, user, vacancyID, false, nil)
	if !errors.Is(err, coverletters.ErrPaidUncertain) || mock.Sends != 1 ||
		count("SELECT COUNT(*) FROM openrouter_ai_logs WHERE task='openai_cover_letter'") != 1 {
		t.Fatal("identical paid input was replayed", err, mock.Sends)
	}
	// Явное изменение входа создаёт новую операцию, старый unknown остаётся.
	f.exec("UPDATE system_prompts SET markdown='CI New Synthetic Prompt',revision=revision+1 WHERE kind='cover_letter'")
	letter, err := svc.Generate(f.ctx, user, vacancyID, false, nil)
	if err != nil || letter.CoverLetterMD != "CI Paid body." || mock.Sends != 2 {
		t.Fatal("new input failed independent paid operation", err, mock.Sends)
	}
	if count("SELECT COUNT(*) FROM openrouter_ai_logs WHERE task='openai_cover_letter' AND error_code LIKE 'paid_unknown:%'") != 1 ||
		count("SELECT COUNT(*) FROM openrouter_ai_logs WHERE task='openai_cover_letter' AND success=1 AND error_code LIKE 'paid_response:%'") != 1 {
		t.Fatal("unknown and acknowledged records were not preserved separately")
	}
	// Простое повторное открытие готового письма не отправляет оплачиваемый POST.
	cached, err := svc.Generate(f.ctx, user, vacancyID, false, nil)
	if err != nil || cached != letter || mock.Sends != 2 {
		t.Fatal("saved paid letter generated again", err, mock.Sends)
	}
	cancelled, cancel := context.WithCancel(f.ctx)
	cancel()
	_, err = svc.Generate(cancelled, user, vacancyID, true, nil)
	if !errors.Is(err, context.Canceled) || mock.Sends != 2 {
		t.Fatal("cancelled paid request reached provider", err, mock.Sends)
	}
}
