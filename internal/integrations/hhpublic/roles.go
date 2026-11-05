package hhpublic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrRolesUnavailable = errors.New("справочник профессиональных ролей HH недоступен или некорректен")

type ProfessionalRole struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ProfessionalRoleCategory struct {
	ID    string             `json:"id"`
	Name  string             `json:"name"`
	Roles []ProfessionalRole `json:"roles"`
}

type ProfessionalRoles struct {
	Categories []ProfessionalRoleCategory `json:"categories"`
}

// FetchProfessionalRoles читает только публичный фиксированный справочник HH.
// Ошибка не должна приводить к очистке ранее сохранённых настроек.
func (c *Client) FetchProfessionalRoles(ctx context.Context) (*ProfessionalRoles, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil || c.http == nil {
		return nil, ErrRolesUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(bounded, http.MethodGet, "https://api.hh.ru/professional_roles", nil)
	if err != nil {
		return nil, ErrRolesUnavailable
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "IT-MAP/1.0")
	req.Header.Set("HH-User-Agent", "IT-MAP/1.0")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrRolesUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > maxBytes || !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		return nil, ErrRolesUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var result ProfessionalRoles
	if err != nil || len(raw) > maxBytes || !utf8.Valid(raw) || json.Unmarshal(raw, &result) != nil || len(result.Categories) == 0 {
		return nil, ErrRolesUnavailable
	}
	categories := map[string]bool{}
	roles := map[string]string{}
	for _, category := range result.Categories {
		if !publicID.MatchString(category.ID) || trim(category.Name) == "" || categories[category.ID] || len(category.Roles) == 0 {
			return nil, ErrRolesUnavailable
		}
		categories[category.ID] = true
		seen := map[string]bool{}
		for _, role := range category.Roles {
			if !publicID.MatchString(role.ID) || trim(role.Name) == "" || seen[role.ID] || (roles[role.ID] != "" && roles[role.ID] != role.Name) {
				return nil, ErrRolesUnavailable
			}
			seen[role.ID] = true
			roles[role.ID] = role.Name
		}
	}
	return &result, nil
}
