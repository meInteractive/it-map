// Package firecrawl implements individual Firecrawl API calls.
// Callers must reserve credits durably before dispatching a scrape.
package firecrawl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const endpoint = "https://api.firecrawl.dev/v2/"
const responseLimit = 8 << 20

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
var blockedPage = regexp.MustCompile(`(?i)captcha|verify (you are|you're) human|access denied|доступ (ограничен|запрещён)|подтвердите.{0,30}(человек|робот)`)

var (
	ErrInput    = errors.New("invalid Firecrawl request")
	ErrNetwork  = errors.New("Firecrawl request outcome unknown")
	ErrResponse = errors.New("invalid Firecrawl response")
	ErrRejected = errors.New("Firecrawl rejected request")
	ErrTarget   = errors.New("Firecrawl target rejected")
)

type Client struct {
	key  string
	http http.Client
}

func New(key string, transport http.RoundTripper) (*Client, error) {
	if key == "" || strings.ContainsAny(key, " \r\n\t") {
		return nil, ErrInput
	}
	return &Client{key: key, http: http.Client{
		Transport:     transport,
		Timeout:       62 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

type CreditUsage struct {
	RemainingCredits *float64 `json:"remainingCredits"`
	PlanCredits      *float64 `json:"planCredits"`
	PeriodStart      string   `json:"billingPeriodStart"`
	PeriodEnd        string   `json:"billingPeriodEnd"`
}

func (c *Client) CreditUsage(ctx context.Context) (CreditUsage, error) {
	var result CreditUsage
	_, err := c.request(ctx, http.MethodGet, "team/credit-usage", nil, &result)
	if err == nil && (result.RemainingCredits == nil || *result.RemainingCredits < 0) {
		err = ErrResponse
	}
	return result, err
}

type ScrapeOptions struct {
	AllowedHosts    []string
	Timeout         time.Duration
	MarkdownOnly    bool
	HTML            bool
	OnlyMainContent bool
}

type ScrapeResult struct {
	Markdown       string
	HTML           string
	Links          []string
	FinalURL       string
	TargetStatus   int
	ProviderStatus int
	// ReportedCredits stays nil when Firecrawl did not report a charge.
	ReportedCredits *float64
}

// Scrape expects a target that has passed public DNS and redirect preflight.
func (c *Client) Scrape(ctx context.Context, target string, options ScrapeOptions) (ScrapeResult, error) {
	if !allowedURL(target, options.AllowedHosts) || options.Timeout < time.Millisecond || options.Timeout > 60*time.Second {
		return ScrapeResult{}, ErrInput
	}
	formats := []string{"markdown"}
	if !options.MarkdownOnly {
		formats = append(formats, "links")
		if options.HTML {
			formats = append(formats, "html")
		}
	}
	payload, err := json.Marshal(map[string]any{
		"url": target, "formats": formats, "timeout": options.Timeout.Milliseconds(),
		"proxy": "basic", "onlyMainContent": options.OnlyMainContent, "onlyCleanContent": false,
		"checkPromptInjection": false, "redactPII": false, "skipTlsVerification": false,
		"parsers": []string{}, "maxAge": 0, "storeInCache": false,
	})
	if err != nil {
		return ScrapeResult{}, ErrInput
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout+2*time.Second)
	defer cancel()
	var data struct {
		Markdown string
		HTML     string
		Links    []string
		Metadata struct {
			URL         string
			SourceURL   string
			StatusCode  int
			ContentType string
			Error       string
			CreditsUsed *float64
			Provider    struct{ CreditsCost *float64 }
		}
	}
	status, err := c.request(ctx, http.MethodPost, "scrape", payload, &data)
	result := ScrapeResult{ProviderStatus: status}
	if err != nil {
		return result, err
	}
	meta := data.Metadata
	result.ReportedCredits = meta.CreditsUsed
	if result.ReportedCredits == nil {
		result.ReportedCredits = meta.Provider.CreditsCost
	}
	if result.ReportedCredits != nil && *result.ReportedCredits < 0 {
		result.ReportedCredits = nil
	}
	result.TargetStatus = meta.StatusCode
	finalURL := meta.URL
	if finalURL == "" {
		finalURL = meta.SourceURL
	}
	if finalURL == "" {
		finalURL = target
	}
	contentType := strings.ToLower(meta.ContentType)
	evidence := data.Markdown[:min(len(data.Markdown), 3000)]
	if !allowedURL(finalURL, options.AllowedHosts) || meta.StatusCode >= 400 || meta.Error != "" || blockedPage.MatchString(evidence) ||
		(contentType != "" && !strings.Contains(contentType, "html") && !strings.HasPrefix(contentType, "text/plain")) {
		return result, ErrTarget
	}
	result.FinalURL, result.Markdown, result.HTML, result.Links = finalURL, data.Markdown, data.HTML, data.Links
	return result, nil
}

func (c *Client) request(ctx context.Context, method, path string, body []byte, result any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint+path, bytes.NewReader(body))
	if err != nil {
		return 0, ErrInput
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return 0, ErrNetwork
	}
	defer response.Body.Close()
	status := response.StatusCode
	if status < 200 || status >= 300 {
		return status, ErrRejected
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil || len(raw) > responseLimit {
		return status, ErrResponse
	}
	var envelope struct {
		Success bool
		Data    json.RawMessage
	}
	if json.Unmarshal(raw, &envelope) != nil || !envelope.Success || len(envelope.Data) == 0 ||
		string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, result) != nil {
		return status, ErrResponse
	}
	return status, nil
}

func allowedURL(raw string, allowed []string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		parsed.User != nil || parsed.Port() != "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if net.ParseIP(host) != nil || len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if !dnsLabel.MatchString(label) {
			return false
		}
	}
	if last := labels[len(labels)-1]; last[0] >= '0' && last[0] <= '9' {
		return false
	}
	for _, suffix := range []string{".localhost", ".local", ".internal", ".test", ".invalid", ".example", ".onion", ".arpa", ".lan", ".home"} {
		if strings.HasSuffix(host, suffix) {
			return false
		}
	}
	for _, entry := range allowed {
		if host == strings.ToLower(strings.TrimSuffix(entry, ".")) {
			return true
		}
	}
	return false
}
