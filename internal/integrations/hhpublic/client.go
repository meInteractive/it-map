package hhpublic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"itmap/internal/retryafter"
)

const maxBytes = 2000000

// ErrTemporary не содержит URL или исходной ошибки сети и допускает повтор GET.
var ErrTemporary = errors.New("public HH temporarily unavailable")

// TemporaryError carries a bounded provider retry hint, without response data.
type TemporaryError struct{ RetryAfter time.Duration }

func (e *TemporaryError) Error() string { return ErrTemporary.Error() }
func (e *TemporaryError) Unwrap() error { return ErrTemporary }

const MinRetryDelay = retryafter.MinDelay
const MaxRetryDelay = retryafter.MaxDelay

func retryAfter(raw string, now time.Time) time.Duration { return retryafter.Parse(raw, now) }

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
		return nil, ErrTemporary
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooEarly || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return nil, &TemporaryError{RetryAfter: retryAfter(response.Header.Get("Retry-After"), time.Now())}
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusForbidden {
		return nil, nil
	}
	if !strings.Contains(response.Header.Get("Content-Type"), "text/html") {
		return nil, nil
	}
	if response.ContentLength > maxBytes {
		if response.StatusCode == http.StatusForbidden {
			return nil, ErrTemporary
		}
		return nil, nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, ErrTemporary
	}
	if len(raw) > maxBytes || !utf8.Valid(raw) {
		if response.StatusCode == http.StatusForbidden {
			return nil, ErrTemporary
		}
		return nil, nil
	}
	body := string(raw)
	if response.StatusCode == http.StatusOK {
		if job := ParseJob(body, id, time.Now()); job != nil {
			return job, nil
		}
	}
	if verificationChallenge(body) {
		return nil, &TemporaryError{RetryAfter: retryAfter(response.Header.Get("Retry-After"), time.Now())}
	}
	return nil, nil
}
