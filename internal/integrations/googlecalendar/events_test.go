package googlecalendar

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type eventTransport struct {
	events map[string]Event
	post int
	get int
	patch int
	dropFirstAck bool
	foreign bool
}

func (f *eventTransport) RoundTrip(req *http.Request) (*http.Response,error) {
	if req.URL.Host!="www.googleapis.com"||req.Header.Get("Authorization")!="Bearer CI_ACCESS" {
		return nil,errors.New("wrong Google host or authorization")
	}
	parts:=strings.Split(strings.Trim(req.URL.Path,"/"),"/")
	id:=parts[len(parts)-1]
	event:=Event{}
	code:=http.StatusOK
	switch req.Method {
	case http.MethodGet:
		f.get++
		var ok bool
		event,ok=f.events[id]
		if !ok {code=http.StatusNotFound}
	case http.MethodPost:
		f.post++
		if json.NewDecoder(req.Body).Decode(&event)!=nil {return nil,errors.New("invalid mock event")}
		if _,ok:=f.events[event.ID];ok {code=http.StatusConflict
		} else {
			event.HTMLLink="https://calendar.google.com/calendar/event?eid=CI"
			f.events[event.ID]=event
		}
		if f.dropFirstAck {
			f.dropFirstAck=false
			return nil,errors.New("CI lost Google ACK after successful insert")
		}
	case http.MethodPatch:
		f.patch++
		if json.NewDecoder(req.Body).Decode(&event)!=nil {return nil,errors.New("invalid mock patch")}
		if event.ID!=id {return nil,errors.New("patch wrong event")}
		f.events[id]=event
	default:
		return nil,errors.New("unexpected method")
	}
	if f.foreign && req.Method==http.MethodGet && code==http.StatusOK {
		event.ExtendedProperties.Private["source"]="foreign-calendar"
	}
	payload,_:=json.Marshal(event)
	return &http.Response{StatusCode:code,Body:io.NopCloser(strings.NewReader(string(payload))),Header:http.Header{}},nil
}

func TestCalendarIdempotentUpsertAndLostACK(t *testing.T) {
	f:=&eventTransport{events:map[string]Event{},dropFirstAck:true}
	client:=TokenClient{HTTP:&http.Client{Transport:f}}
	in:=EventInput{ID:12,User:"CI_USER",Company:"CI COMPANY",Type:"meeting",Note:"CI appointment",DueAt:"2026-12-01T12:00:00.000Z"}
	wanted,err:=in.Build()
	if err!=nil || len(wanted.ID)!=65 || wanted.ExtendedProperties.Private["taskKey"]!=wanted.ID {
		t.Fatal("wrong stable Event ID",err)
	}
	if second,_:=in.Build();second.ID!=wanted.ID {t.Fatal("Event ID unstable")}
	_,err=client.Upsert(context.Background(),"CI_ACCESS","primary",wanted)
	if !errors.Is(err,ErrEventUnknown)||f.post!=1 {t.Fatal("ACK lost was not treated as unknown",err,f.post)}
	event,err:=client.Upsert(context.Background(),"CI_ACCESS","primary",wanted)
	if err!=nil||event.ID!=wanted.ID||f.post!=1 {t.Fatal("retry made duplicate Google POST",err,f.post)}
	in.DueAt="2026-12-01T13:00:00.000Z"
	updated,_:=in.Build()
	if _,err=client.Upsert(context.Background(),"CI_ACCESS","primary",updated);err!=nil||f.patch!=1||f.post!=1 {
		t.Fatal("reschedule created a new event",err,f.post,f.patch)
	}
	if _,err=client.Upsert(context.Background(),"CI_ACCESS","primary",updated);err!=nil||f.patch!=1 {
		t.Fatal("unchanged event should not PATCH",err)
	}
	f.foreign=true
	if _,err=client.Upsert(context.Background(),"CI_ACCESS","primary",updated);!errors.Is(err,ErrEventConflict) {
		t.Fatal("foreign event identity accepted",err)
	}
}

func TestCalendarEventValidationAndSafeLink(t *testing.T){
	for _,tc:=range []EventInput{
		{ID:0,User:"CI_USER",Company:"CI",Type:"call",DueAt:"2026-12-01T12:00:00Z"},
		{ID:1,User:"",Company:"CI",Type:"call",DueAt:"2026-12-01T12:00:00Z"},
		{ID:1,User:"CI_USER",Company:"CI",Type:"unknown",DueAt:"2026-12-01T12:00:00Z"},
		{ID:1,User:"CI_USER",Company:"CI",Type:"call",DueAt:"not a date"},
	}{
		if _,err:=tc.Build();err==nil {t.Fatal("invalid event accepted",tc)}
	}
	if SafeEventLink("https://calendar.google.com/calendar/event?eid=CI") == "" ||
		SafeEventLink("https://google.com.evil.invalid/calendar")!="" ||
		SafeEventLink("javascript:alert(1)")!="" ||
		SafeEventLink("https://user@google.com/calendar")!="" {
		t.Fatal("unsafe or valid Google link misclassified")
	}
}

func TestCalendarConfirmedDuplicateIDCannotOverwriteForeignEvent(t *testing.T) {
	f:=&eventTransport{events:map[string]Event{}}
	client:=TokenClient{HTTP:&http.Client{Transport:f}}
	wanted,_:=(EventInput{ID:99,User:"CI",Company:"Example",Type:"call",DueAt:"2026-12-01T12:00:00Z"}).Build()
	foreign:=wanted
	foreign.ExtendedProperties=EventProperties{Private:map[string]string{"source":"foreign"}}
	f.events[wanted.ID]=foreign
	_,err:=client.Upsert(context.Background(),"CI_ACCESS","primary",wanted)
	if !errors.Is(err,ErrEventConflict)||f.post!=0||f.patch!=0 {
		t.Fatal("foreign event collision modified",err,f.post,f.patch)
	}
}
