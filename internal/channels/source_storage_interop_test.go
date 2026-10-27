//go:build legacyinterop

package channels

import (
	"encoding/json"
	"os/exec"
	"sort"
	"testing"
)

func TestLegacyChannelStorageOrder(t *testing.T) {
	out, e := exec.Command("node", "../../scripts/check-channel-storage.mjs", "--stdio").Output()
	if e != nil {
		t.Fatal("required channel storage source unavailable")
	}
	var frame struct {
		OrderNames []string
		Ordered    []Channel
		FoldPairs  []struct {
			A, B  string
			Equal bool
		}
	}
	if json.Unmarshal(out, &frame) != nil {
		t.Fatal("invalid channel source frame")
	}
	rows := make([]Channel, len(frame.OrderNames))
	for i, name := range frame.OrderNames {
		rows[i] = Channel{ID: int64(100 + i), Name: name}
	}
	sort.SliceStable(rows, func(i, j int) bool { return sourceChannelOrder(rows[i], rows[j]) })
	if len(rows) != 12 || len(rows) != len(frame.Ordered) {
		t.Fatal("missing source order coverage")
	}
	for i, row := range rows {
		if row.ID != frame.Ordered[i].ID {
			t.Fatal("channel NOCASE order differs from source SQLite")
		}
	}
	if len(frame.FoldPairs) != 400 {
		t.Fatal("source LOWER coverage incomplete")
	}
	for _, v := range frame.FoldPairs {
		if (asciiLower(v.A) == asciiLower(v.B)) != v.Equal {
			t.Fatal("channel duplicate name fold differs from SQLite LOWER")
		}
	}
}
