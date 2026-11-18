//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"itmap/internal/integrations/googlecalendar"
	"itmap/internal/legacycrypto"
	"itmap/internal/tasks"
)

type calendarRoundTrip func(*http.Request) (*http.Response, error)

func (f calendarRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMariaDBGoogleCalendarManualSyncDisposable(t *testing.T) {
	f := openSchedulerFixture(t)
	if f.count("SELECT COUNT(*) FROM company_tasks WHERE id IN (990236,990237)") != 0 ||
		f.count("SELECT COUNT(*) FROM company_crm WHERE company_name='CI GOOGLE COMPANY'") != 0 {
		t.Fatal("CI calendar namespace already present")
	}
	f.exec("INSERT INTO company_crm(company_name) VALUES('CI GOOGLE COMPANY')")
	f.exec(`INSERT INTO company_tasks(id,user_name,company_name,type,note,due_at,google_sync_status)
	VALUES(990236,'CI_GOOGLE_USER','CI GOOGLE COMPANY','meeting','CI NOTES','2026-11-19T12:00:00.000Z','pending'),
	(990237,'CI_OTHER','CI GOOGLE COMPANY','call','CI OTHER NOTES','2026-11-19T12:00:00.000Z','pending')`)
	codec, err := legacycrypto.New(strings.Repeat("C", 64), legacycrypto.Calendar)
	if err != nil { t.Fatal(err) }
	token, err := codec.Encrypt("CI_REFRESH_TOKEN")
	if err != nil { t.Fatal(err) }
	f.exec(`INSERT INTO google_calendar_user_connection(user_name,account_email,refresh_token_encrypted,calendar_id)
	VALUES('CI_GOOGLE_USER','ci@example.invalid',?,'primary')`,token)
	t.Cleanup(func() {
		f.exec("DELETE FROM google_calendar_user_connection WHERE user_name='CI_GOOGLE_USER'")
		f.exec("DELETE FROM company_tasks WHERE id IN (990236,990237)")
		f.exec("DELETE FROM company_crm WHERE company_name='CI GOOGLE COMPANY'")
	})
	wantedID := googlecalendar.EventID("CI_GOOGLE_USER",990236)
	events := map[string]googlecalendar.Event{}
	posts,gets,patches := 0,0,0
	dropACK := true
	svc := googlecalendar.Service{
		DB:f.db, Secret: strings.Repeat("C",64),
		OAuth:googlecalendar.Config{
			ClientID:"CI_CLIENT",ClientSecret:"CI_CLIENT_SECRET",
			RedirectURI:"https://example.invalid/api/google-calendar/callback",
			StateSecret:strings.Repeat("C",64),
		},
		Tokens:googlecalendar.TokenClient{HTTP:&http.Client{Transport:calendarRoundTrip(func(req *http.Request)(*http.Response,error) {
			if req.URL.Host=="oauth2.googleapis.com" {
				if req.Method!="POST" {t.Fatal("unexpected token method")}
				return &http.Response{StatusCode:200,Body:io.NopCloser(strings.NewReader(`{"access_token":"CI_ACCESS","token_type":"Bearer","expires_in":3599}`)),Header:http.Header{}},nil
			}
			if req.URL.Host!="www.googleapis.com"||req.Header.Get("Authorization")!="Bearer CI_ACCESS" {
				t.Fatal("unexpected request target or missing Google auth header",req.URL.Host)
			}
			out := googlecalendar.Event{}
			code := http.StatusOK
			switch req.Method {
			case http.MethodGet:
				gets++
				v,ok:=events[wantedID]
				if !ok {code=http.StatusNotFound} else {out=v}
			case http.MethodPost:
				posts++
				if json.NewDecoder(req.Body).Decode(&out)!=nil || out.ID!=wantedID {
					t.Fatal("unexpected event creation")
				}
				out.HTMLLink="https://calendar.google.com/calendar/event?eid=CI"
				events[wantedID]=out
				if dropACK {
					dropACK=false
					return nil,errors.New("CI_NETWORK_DROPPED_ACK")
				}
			case http.MethodPatch:
				patches++
				if json.NewDecoder(req.Body).Decode(&out)!=nil || out.ID!=wantedID {
					t.Fatal("wrong event patch")
				}
				out.HTMLLink="https://calendar.google.com/calendar/event?eid=CI"
				events[wantedID]=out
			default:
				t.Fatal("unexpected request",req.Method)
			}
			payload,_:=json.Marshal(out)
			return &http.Response{StatusCode:code,Body:io.NopCloser(strings.NewReader(string(payload))),Header:http.Header{}},nil
		})}},
	}
	status,err:=svc.Status(f.ctx,"CI_GOOGLE_USER")
	if err!=nil||!status.Configured||!status.Connected||status.AccountEmail!="ci@example.invalid"||status.SyncedTasks!=0 {
		t.Fatal("incorrect initial connection status",err,status)
	}
	first,err:=svc.Sync(f.ctx,"CI_GOOGLE_USER")
	if err!=nil||first.Errors!=1||first.Synced!=0||posts!=1 {
		t.Fatal("lost ACK was not preserved as error",err,first,posts)
	}
	if f.count("SELECT COUNT(*) FROM company_tasks WHERE id=990236 AND google_sync_status='error' AND google_event_id IS NULL AND google_sync_error='google_event_unknown'")!=1 {
		t.Fatal("unknown ACK was lost or written as success")
	}
	second,err:=svc.Sync(f.ctx,"CI_GOOGLE_USER")
	if err!=nil||second.Synced!=1||second.Errors!=0||posts!=1||gets<2 {
		t.Fatal("retry duplicated POST instead of GET by Event ID",err,second,posts,gets)
	}
	if f.count("SELECT COUNT(*) FROM company_tasks WHERE id=990236 AND google_sync_status='synced' AND google_event_id='"+wantedID+"'")!=1 ||
		f.count("SELECT COUNT(*) FROM company_tasks WHERE id=990237 AND google_event_id IS NOT NULL")!=0 {
		t.Fatal("sync changed wrong task, user or stored no Event ID")
	}
	_,err=(tasks.Repository{DB:f.db}).Reschedule(context.Background(),"CI_GOOGLE_USER",990236,"2026-11-19T15:00:00Z")
	if err!=nil || f.count("SELECT COUNT(*) FROM company_tasks WHERE id=990236 AND google_sync_status='pending'")!=1 {
		t.Fatal("connected reschedule was not marked pending",err)
	}
	third,err:=svc.Sync(f.ctx,"CI_GOOGLE_USER")
	if err!=nil||third.Synced!=1||patches!=1||posts!=1 {
		t.Fatal("reschedule created duplicate event or skipped PATCH",err,third,patches,posts)
	}
	if err:=svc.Disconnect(f.ctx,"CI_GOOGLE_USER");err!=nil {t.Fatal(err)}
	disconnected,err:=svc.Status(f.ctx,"CI_GOOGLE_USER")
	if err!=nil||disconnected.Connected||disconnected.SyncedTasks!=1 {
		t.Fatal("disconnect deleted tasks or kept refresh token",err,disconnected)
	}
	if _,err=svc.Sync(f.ctx,"CI_GOOGLE_USER");!errors.Is(err,googlecalendar.ErrDisconnected) {
		t.Fatal("disconnected sync attempted Google network",err)
	}
}
