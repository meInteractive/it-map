package parser

import (
	"encoding/json"
	"itmap/internal/aiproviders"
	"itmap/internal/backup"
	"itmap/internal/profiles"
)

func PrepareLeafRoleRequest(prompt, title, markdown string, allowed []aiproviders.LeafRole, config profiles.ModelInput) (aiproviders.GenerationRequest, error) {
	if len(allowed) == 0 {
		return aiproviders.GenerationRequest{}, &aiproviders.ServiceError{Code: "no_leaf_roles", Status: 409, Message: "В подходящих направлениях нет дочерних конечных ролей."}
	}
	if utf16Length(markdown) > ExtractionInputLimit {
		return aiproviders.GenerationRequest{}, &aiproviders.ServiceError{Code: "input_too_large", Status: 422, Message: "Markdown сохранён целиком, но превышает безопасный лимит LLM."}
	}
	if jsStageTrim(prompt) == "" {
		return aiproviders.GenerationRequest{}, &aiproviders.ServiceError{Code: "empty_prompt", Status: 409, Message: "Заполните промт определения роли."}
	}
	if e := profiles.ValidateModel("vacancy_role", config); e != nil {
		return aiproviders.GenerationRequest{}, e
	}
	roles := append([]aiproviders.LeafRole(nil), allowed...)
	seen := map[int64]bool{}
	ids := []int64{}
	for _, role := range roles {
		if role.ID < 1 || role.ID > 9007199254740991 || jsStageTrim(role.Path) == "" || seen[role.ID] {
			return aiproviders.GenerationRequest{}, ErrJobInput
		}
		seen[role.ID] = true
		ids = append(ids, role.ID)
	}
	validate := func(content string) error { _, e := ValidateLeafRoleIDs(content, roles); return e }
	req := aiproviders.GenerationRequest{Task: "vacancy_role", MaxTokens: 4096, ValidateContent: validate}
	if config.Provider == "openrouter" && config.OpenRouterModel == "jev" {
		req.Instructions = prompt
		req.State = struct {
			Title    string `json:"title"`
			Markdown string `json:"markdown"`
		}{title, markdown}
		req.Roles = roles
		return req, nil
	}
	schemaRaw, e := json.Marshal(struct {
		Type       string   `json:"type"`
		Additional bool     `json:"additionalProperties"`
		Required   []string `json:"required"`
		Properties struct {
			Roles struct {
				Type  string `json:"type"`
				Items struct {
					Type string  `json:"type"`
					Enum []int64 `json:"enum"`
				} `json:"items"`
			} `json:"role_ids"`
		} `json:"properties"`
	}{Type: "object", Required: []string{"role_ids"}, Properties: struct {
		Roles struct {
			Type  string `json:"type"`
			Items struct {
				Type string  `json:"type"`
				Enum []int64 `json:"enum"`
			} `json:"items"`
		} `json:"role_ids"`
	}{Roles: struct {
		Type  string `json:"type"`
		Items struct {
			Type string  `json:"type"`
			Enum []int64 `json:"enum"`
		} `json:"items"`
	}{Type: "array", Items: struct {
		Type string  `json:"type"`
		Enum []int64 `json:"enum"`
	}{"integer", ids}}}})
	if e != nil {
		return aiproviders.GenerationRequest{}, ErrJobInput
	}
	schema, e := backup.StringifyParsedJSON(schemaRaw)
	if e != nil {
		return aiproviders.GenerationRequest{}, ErrSettingsStorage
	}
	body, e := json.Marshal(struct {
		Title    string                 `json:"title"`
		Markdown string                 `json:"markdown"`
		Allowed  []aiproviders.LeafRole `json:"allowedRoles"`
	}{title, markdown, roles})
	if e != nil {
		return aiproviders.GenerationRequest{}, ErrJobInput
	}
	body, e = backup.StringifyParsedJSON(body)
	if e != nil {
		return aiproviders.GenerationRequest{}, ErrJobInput
	}
	format, e := json.Marshal(struct {
		Type string `json:"type"`
		Spec struct {
			Name   string          `json:"name"`
			Strict bool            `json:"strict"`
			Schema json.RawMessage `json:"schema"`
		} `json:"json_schema"`
	}{Type: "json_schema", Spec: struct {
		Name   string          `json:"name"`
		Strict bool            `json:"strict"`
		Schema json.RawMessage `json:"schema"`
	}{"vacancy_leaf_roles", true, schema}})
	if e != nil {
		return aiproviders.GenerationRequest{}, ErrJobInput
	}
	req.ResponseFormat = format
	req.Messages = []aiproviders.Message{{Role: "system", Content: prompt + "\nСхема ответа: " + string(schema)}, {Role: "user", Content: string(body)}}
	return req, nil
}
