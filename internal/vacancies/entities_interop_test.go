//go:build legacyinterop

package vacancies

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"reflect"
	"testing"
)

func TestVacancyEntitiesSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-vacancy-entities.mjs")
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Start() != nil {
		t.Fatal("vacancy entity source not started")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder, count := json.NewDecoder(pipe), 0
	for {
		var f struct {
			Name     string
			Raw      *string
			Names    []string
			Expected json.RawMessage
			Error    bool
		}
		err = decoder.Decode(&f)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
		t.Run(f.Name, func(t *testing.T) {
			var data []byte
			if f.Raw != nil {
				data = []byte(*f.Raw)
			}
			selected := append(append([]string{}, f.Names...), "", "")
			got, err := detailEntities(data, selected[0], selected[1])
			if (err != nil) != f.Error {
				t.Fatal("source entity error differs")
			}
			if err != nil {
				return
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var actual, want any
			if json.Unmarshal(raw, &actual) != nil || json.Unmarshal(f.Expected, &want) != nil || !reflect.DeepEqual(actual, want) {
				t.Fatal("source normalized entity fields/order/shape differ")
			}
		})
	}
	if cmd.Wait() != nil || count != 180 {
		t.Fatal("incomplete vacancy entity source corpus", count)
	}
}
