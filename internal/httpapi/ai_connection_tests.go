package httpapi

import (
	"context"
	"itmap/internal/integrations/aihttp"
	"net/http"
	"time"
)

type AIConnectionTests interface {
	Test(context.Context, aihttp.Provider) (map[string]any, error)
}

func runAIConnectionTest(w http.ResponseWriter, r *http.Request, p aihttp.Provider, tests []AIConnectionTests) {
	if len(tests) == 0 || tests[0] == nil {
		jsonReply(w, 503, map[string]string{"error": "Тестовый запрос ещё не подключён.", "code": "backend_not_migrated"})
		return
	}
	// Только явный POST администратора; auth/origin проверены внешним handler.
	ctx, cancel := context.WithTimeout(r.Context(), 335*time.Second)
	defer cancel()
	// Не меняем timeout остальных routes; httptest может не поддерживать deadline.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(340 * time.Second))
	result, e := tests[0].Test(ctx, p)
	if e != nil {
		if p == aihttp.OpenRouter {
			routerSettingsError(w, e)
		} else {
			aiSettingsError(w, e)
		}
		return
	}
	jsonReply(w, 200, result)
}
