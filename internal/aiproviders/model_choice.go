package aiproviders

import (
	"context"
	"errors"
	"itmap/internal/integrations/aihttp"
	"itmap/internal/profiles"
)

func (s *OpenAIService) ValidateChoice(ctx context.Context, kind string, input profiles.ModelInput) error {
	if e := profiles.ValidateModel(kind, input); e != nil {
		return e
	}
	if input.Provider != "openai" {
		return &profiles.InputError{Status: 503, Message: "Каталог JEV ещё не подключён."}
	}
	if input.OpenAIModel != "" && !TextModel(input.OpenAIModel) {
		return &profiles.InputError{Status: 400, Message: "Выберите текстовую модель OpenAI."}
	}
	if input.OpenAIModel != "" && input.ReasoningEffort != "auto" {
		valid := false
		for _, v := range ModelProfile(input.OpenAIModel).ReasoningEfforts {
			if v == input.ReasoningEffort {
				valid = true
			}
		}
		if !valid {
			return &profiles.InputError{Status: 400, Message: "Этот уровень рассуждений не подтверждён для выбранной модели. Выберите «По умолчанию модели»."}
		}
	}
	available, e := s.Models(ctx, false)
	if e != nil {
		status, message := 503, providerWarning(e)
		var provider *aihttp.Error
		if errors.As(e, &provider) {
			status = provider.Status
		}
		if errors.Is(e, ErrMissing) || errors.Is(e, ErrDecrypt) {
			status = 409
		}
		return &profiles.InputError{Status: status, Message: message}
	}
	if input.OpenAIModel != "" {
		found := false
		for _, m := range available {
			if m.ID == input.OpenAIModel {
				found = true
			}
		}
		if !found {
			return &profiles.InputError{Status: 400, Message: "Выбранная модель отсутствует в списке доступных OpenAI API-моделей."}
		}
	}
	if len(available) == 0 {
		return &profiles.InputError{Status: 409, Message: "Нет доступных текстовых моделей OpenAI."}
	}
	return nil
}
