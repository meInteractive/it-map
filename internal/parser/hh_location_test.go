package parser

import (
	"encoding/json"
	"testing"
)

func TestHHMoscowLocation(t *testing.T) {
	for _, tc := range []struct {
		raw            string
		match, invalid bool
	}{
		{`{"location":"Москва"}`, true, false}, {`{"location":"Москве"}`, true, false}, {`{"location":"Москве, офис"}`, true, false}, {`{"location":"МОСКВА, Тверская улица"}`, true, false}, {`{"location":"г. Москва"}`, true, false}, {`{"location":"Moscow, office"}`, true, false},
		{`{"location":"Санкт-Петербург"}`, false, false}, {`{"location":"Химки, Московская область"}`, false, false}, {`{"location":"Москва и Московская область"}`, false, true},
		{`{"location":"Россия, Москве"}`, false, true}, {`{"location":"Россия, Москва"}`, false, true}, {`{"location":"Удалённо"}`, false, true},
		{`{"location":""}`, false, true}, {`{}`, false, true}, {`{"location":null}`, false, true}, {`{"location":123}`, false, true},
	} {
		got, err := hhMoscowLocation(json.RawMessage(tc.raw))
		if got != tc.match || (err != nil) != tc.invalid {
			t.Fatalf("location scope: match=%v invalid=%v", got, err != nil)
		}
	}
}
