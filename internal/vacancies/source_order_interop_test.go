//go:build legacyinterop

package vacancies

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"
)

func TestVacancyTelegramSourceOrderSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-vacancy-source-order.mjs")
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder, count := json.NewDecoder(pipe), 0
	for {
		var frame struct {
			Name     string
			Channels []struct {
				ID                  int64
				Name, URL, Username string
			}
			Sources []struct {
				ID, Channel      int64
				Message, Contact string
			}
			Expected map[string]json.RawMessage
		}
		err = decoder.Decode(&frame)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
		sort.Slice(frame.Sources, func(i, j int) bool { return frame.Sources[i].ID < frame.Sources[j].ID })
		compact, full := []TelegramSource{}, []DetailTelegramSource{}
		for _, s := range frame.Sources {
			for _, c := range frame.Channels {
				if c.ID == s.Channel {
					compact = append(compact, TelegramSource{ChannelID: c.ID, ChannelName: c.Name})
					if s.Message != "" {
						full = append(full, DetailTelegramSource{ChannelID: c.ID, ChannelName: c.Name, ChannelURL: c.URL, ChannelUsername: c.Username, MessageURL: s.Message, TelegramContact: s.Contact, id: strconv.FormatInt(s.ID, 10)})
					}
				}
			}
		}
		before, _ := json.Marshal(full)
		rawRevisionRows := telegramRevisionRows(full)
		detail := Detail{TelegramSources: full}
		revision, err := DetailRevision(detail, rawRevisionRows)
		if err != nil {
			t.Fatal(err)
		}
		ordered := orderedDetailTelegramSources(full)
		detail.TelegramSources = ordered
		next, err := DetailRevision(detail, rawRevisionRows)
		if err != nil || revision != next {
			t.Fatal("display order changed revision")
		}
		after, _ := json.Marshal(full)
		if string(before) != string(after) || !reflect.DeepEqual(rawRevisionRows, telegramRevisionRows(full)) {
			t.Fatal("display order mutated provenance")
		}
		compactBefore, _ := json.Marshal(compact)
		body := BuildList([]Vacancy{{ID: 1}}, nil, map[int64][]TelegramSource{1: compact}, nil, nil, nil, nil, nil, time.Time{})
		compactAfter, _ := json.Marshal(compact)
		if string(compactBefore) != string(compactAfter) {
			t.Fatal("list source order mutated input")
		}
		for _, view := range []string{"list", "detail", "company"} {
			t.Run(frame.Name+"/"+view, func(t *testing.T) {
				var actual any = ordered
				if view == "list" {
					actual = body.Vacancies[0].TelegramSources
				}
				raw, err := json.Marshal(actual)
				if err != nil {
					t.Fatal(err)
				}
				var got, want any
				if json.Unmarshal(raw, &got) != nil || json.Unmarshal(frame.Expected[view], &want) != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("source order/fields differ: got %s want %s", raw, frame.Expected[view])
				}
			})
		}
	}
	if cmd.Wait() != nil || count != 57 {
		t.Fatal("incomplete Telegram source corpus", count)
	}
}
