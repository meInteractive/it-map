package parser

import "errors"

type firecrawlStageError struct {
	code string
	err  error
}

func (e *firecrawlStageError) Error() string { return e.code }
func (e *firecrawlStageError) Unwrap() error { return e.err }

func firecrawlFailure(code string, err error) error {
	if err == nil {
		return nil
	}
	return &firecrawlStageError{code: code, err: err}
}

func firecrawlFailureDetails(err error) (code, message string) {
	var stage *firecrawlStageError
	if !errors.As(err, &stage) {
		return "", ""
	}
	switch stage.code {
	case "firecrawl_key":
		return stage.code, "Проверьте активный профиль и ключ Firecrawl."
	case "firecrawl_preflight":
		return stage.code, "Предварительная проверка адреса источника не прошла."
	case "firecrawl_balance":
		return stage.code, "Не удалось прочитать баланс Firecrawl."
	case "firecrawl_budget":
		return stage.code, "Firecrawl не смог зарезервировать кредиты в текущем расчётном периоде."
	case "firecrawl_reserved":
		return stage.code, "Для этого шага уже есть резерв Firecrawl. Повторный запрос не отправлен."
	case "firecrawl_reserve":
		return stage.code, "Не удалось подготовить резерв Firecrawl."
	case "firecrawl_scrape":
		return stage.code, "Не удалось получить результат сканирования Firecrawl."
	case "firecrawl_settlement":
		return stage.code, "Не удалось записать списание Firecrawl."
	case "firecrawl_target":
		return stage.code, "Не подтверждён успешный ответ страницы источника."
	case "firecrawl_listing":
		return stage.code, "В ответе не удалось подтвердить ссылки на вакансии."
	case "firecrawl_content":
		return stage.code, "В ответе не удалось подтвердить текст вакансии."
	case "firecrawl_save":
		return stage.code, "Не удалось сохранить результат Firecrawl."
	case "firecrawl_source":
		return stage.code, "Настройки или состояние источника изменились либо не поддерживаются."
	}
	return "", ""
}
