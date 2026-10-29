//go:build legacyinterop

package vacancies

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"testing"
	"time"

	"itmap/internal/roles"
)

func TestAssignedVacancyRolesSource(t *testing.T) {
	cmd := exec.Command("node", "../../scripts/check-assigned-role-order.mjs")
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Start() != nil {
		t.Fatal("source role oracle not started")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder, count := json.NewDecoder(pipe), 0
	for {
		var frame struct {
			Name        string
			Roles       []roles.Role
			Assignments []struct {
				VacancyID int64
				RoleID    int64
			}
			Vacancies []struct {
				ID   int64
				Role string
			}
			Expected []struct {
				ID    int64
				Role  string
				Roles []roles.Role
			}
		}
		err = decoder.Decode(&frame)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
		t.Run(frame.Name, func(t *testing.T) {
			byID := map[int64]roles.Role{}
			for _, role := range frame.Roles {
				byID[role.ID] = role
			}
			sort.Slice(frame.Assignments, func(i, j int) bool { return frame.Assignments[i].RoleID < frame.Assignments[j].RoleID })
			selected := map[int64][]roles.Role{}
			for _, a := range frame.Assignments {
				if role, ok := byID[a.RoleID]; ok {
					selected[a.VacancyID] = append(selected[a.VacancyID], role)
				}
			}
			before, _ := json.Marshal(selected)
			input := []Vacancy{}
			for _, v := range frame.Vacancies {
				input = append(input, Vacancy{ID: v.ID, Role: v.Role})
			}
			got := BuildList(input, selected, nil, nil, nil, nil, nil, nil, time.Time{})
			if len(got.Vacancies) != len(frame.Expected) {
				t.Fatal("vacancy count differs")
			}
			for i, want := range frame.Expected {
				v := got.Vacancies[i]
				if v.ID != want.ID || v.Role != want.Role || !reflect.DeepEqual(v.Roles, want.Roles) {
					t.Fatalf("assigned role source order/label differs for vacancy %d", want.ID)
				}
			}
			after, _ := json.Marshal(selected)
			if string(before) != string(after) {
				t.Fatal("role projection mutated input assignments")
			}
		})
	}
	if cmd.Wait() != nil || count != 167 {
		t.Fatal("incomplete assigned role source corpus", count)
	}
}
