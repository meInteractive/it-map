package googlecalendar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrEventConflict = errors.New("google calendar event identity conflict")
	ErrEventUnknown  = errors.New("google calendar request outcome unknown")
)

type Event struct {
	ID                 string            `json:"id"`
	HTMLLink           string            `json:"htmlLink,omitempty"`
	Status             string            `json:"status,omitempty"`
	Summary            string            `json:"summary,omitempty"`
	Description        string            `json:"description,omitempty"`
	Start              EventTime         `json:"start,omitempty"`
	End                EventTime         `json:"end,omitempty"`
	ExtendedProperties EventProperties   `json:"extendedProperties,omitempty"`
}

type EventTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

type EventProperties struct {
	Private map[string]string `json:"private"`
}

type EventInput struct {
	ID       int64
	User     string
	Company  string
	Type     string
	Note     string
	DueAt    string
}

// Ключ не зависит от SESSION_SECRET и его ротации. Один task ID всегда
// означает одну внешнюю идентичность; поля события могут изменяться.
func EventID(user string, taskID int64) string {
	sum := sha256.Sum256([]byte("itmap-calendar-event-v1\x00" + user + "\x00" + strconv.FormatInt(taskID, 10)))
	return "i" + hex.EncodeToString(sum[:])
}

func (in EventInput) Build() (Event, error) {
	if in.ID < 1 || !validUser(in.User) || in.Company == "" || len(in.Company) > 255 {
		return Event{}, ErrEventConflict
	}
	due, err := time.Parse(time.RFC3339Nano, in.DueAt)
	if err != nil {
		return Event{}, ErrEventConflict
	}
	label := map[string]string{"call":"Звонок","email":"Письмо","messenger":"Сообщение","meeting":"Встреча"}[in.Type]
	if label == "" {
		return Event{}, ErrEventConflict
	}
	title := label + ": " + in.Company
	description := in.Note
	if len(description) > 8000 {
		description = string([]rune(description)[:min(2000,len([]rune(description)))])
	}
	start := due.UTC()
	return Event{
		ID:EventID(in.User,in.ID),Summary:title,Description:description,
		Start:EventTime{DateTime:start.Format(time.RFC3339),TimeZone:"Etc/UTC"},
		End:EventTime{DateTime:start.Add(30*time.Minute).Format(time.RFC3339),TimeZone:"Etc/UTC"},
		ExtendedProperties:EventProperties{Private:map[string]string{"source":"itmap","taskKey":EventID(in.User,in.ID)}},
	},nil
}

func matchesEvent(remote Event, wanted Event) bool {
	return remote.ID == wanted.ID && remote.Status != "cancelled" &&
		remote.ExtendedProperties.Private["source"] == "itmap" &&
		remote.ExtendedProperties.Private["taskKey"] == wanted.ID
}

func (t TokenClient) eventCall(ctx context.Context, accessToken, calendarID, method, eventID string, payload *Event) (Event, int, error) {
	if accessToken == "" || len(accessToken) > 8192 || (eventID != "" && len(eventID) > 1024) {
		return Event{}, 0, ErrProvider
	}
	if calendarID == "" {
		calendarID = "primary"
	}
	if len(calendarID) > 300 {
		return Event{}, 0, ErrProvider
	}
	u := "https://www.googleapis.com/calendar/v3/calendars/"+url.PathEscape(calendarID)+"/events"
	if eventID != "" {
		u += "/"+url.PathEscape(eventID)
	}
	u += "?sendUpdates=none"
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return Event{}, 0, ErrProvider
		}
		body = bytes.NewReader(b)
	}
	client := t.HTTP
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	safe := *client
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return Event{}, 0, ErrProvider
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := safe.Do(req)
	if err != nil {
		// После сетевой ошибки даже успешный POST мог достигнуть Google.
		return Event{}, 0, ErrEventUnknown
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		// Код 404 или 409 следует обрабатывать только по явному статусу.
		return Event{}, resp.StatusCode, ErrProvider
	}
	var result Event
	if json.NewDecoder(io.LimitReader(resp.Body, 32*1024)).Decode(&result) != nil {
		return Event{}, resp.StatusCode, ErrEventUnknown
	}
	return result, resp.StatusCode, nil
}

// Upsert с детерминированным внешним ID: любое повторение сперва проверяет
// существующий объект. При 409 подтверждаем, что найден именно наш task;
// сетевой сбой не разрешает создавать второй объект с иным ID.
func (t TokenClient) Upsert(ctx context.Context, accessToken, calendarID string, wanted Event) (Event, error) {
	if wanted.ID == "" || wanted.ExtendedProperties.Private["taskKey"] != wanted.ID {
		return Event{}, ErrEventConflict
	}
	remote, code, err := t.eventCall(ctx, accessToken, calendarID, http.MethodGet, wanted.ID, nil)
	if err == nil {
		if !matchesEvent(remote, wanted) {
			return Event{}, ErrEventConflict
		}
		if remote.Summary == wanted.Summary && remote.Description == wanted.Description &&
			remote.Start == wanted.Start && remote.End == wanted.End {
			return remote, nil
		}
		patch := wanted
		updated, _, e := t.eventCall(ctx, accessToken, calendarID, http.MethodPatch, wanted.ID, &patch)
		if e != nil {
			return Event{}, ErrEventUnknown
		}
		if !matchesEvent(updated,wanted) {
			return Event{}, ErrEventConflict
		}
		return updated, nil
	}
	if code != http.StatusNotFound {
		return Event{}, ErrEventUnknown
	}
	inserted, code, err := t.eventCall(ctx, accessToken, calendarID, http.MethodPost, "", &wanted)
	if err == nil {
		if !matchesEvent(inserted,wanted) {
			return Event{}, ErrEventConflict
		}
		return inserted, nil
	}
	if code == http.StatusConflict {
		again, _, e := t.eventCall(ctx, accessToken, calendarID, http.MethodGet, wanted.ID, nil)
		if e == nil && matchesEvent(again,wanted) {
			return again,nil
		}
	}
	return Event{}, ErrEventUnknown
}

// Письмо и местоположение не отправляем. Лишний внешний event не создаётся,
// если Google вернул отказ или неясный исход.
func SafeEventLink(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || (!strings.HasSuffix(u.Hostname(), ".google.com") && u.Hostname() != "google.com") || u.User != nil {
		return ""
	}
	return s
}
