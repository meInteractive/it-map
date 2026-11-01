package hhpublic

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const maxBytes = 2000000

type Client struct{ http *http.Client }

func New() *Client {
	return &Client{http: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Fetch использует только публичный фиксированный HH endpoint. Недоступный или
// непроверенный ответ означает отсутствие бесплатного снимка, а не успешный текст.
func (c *Client) Fetch(ctx context.Context, value string) (*Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	canonical, id, valid := CanonicalURL(value)
	if !valid || c == nil || c.http == nil {
		return nil, nil
	}
	bounded, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(bounded, http.MethodGet, canonical, nil)
	if err != nil {
		return nil, nil
	}
	request.Header.Set("Accept", "text/html")
	request.Header.Set("User-Agent", "it-map-vacancy-reader/1.0")
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(response.Header.Get("Content-Type"), "text/html") || response.ContentLength > maxBytes {
		return nil, nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || len(raw) > maxBytes || !utf8.Valid(raw) {
		return nil, nil
	}
	return ParseJob(string(raw), id, time.Now()), nil
}
