//go:build legacyinterop

package backup

import (
	"encoding/json"
	"os/exec"
	"reflect"
	"slices"
	"sort"
	"testing"
)

func TestLegacyStoredCatalogSources(t *testing.T) {
	out, e := exec.Command("node", "../../scripts/check-channel-storage.mjs", "--stdio").Output()
	if e != nil {
		t.Fatal("required stored catalog source unavailable")
	}
	var f struct {
		CatalogCases []struct {
			Name, Data    string
			OK            bool
			Expected      []string
			ExpectedNames []string
		}
	}
	if json.Unmarshal(out, &f) != nil {
		t.Fatal("invalid catalog source frame")
	}
	if len(f.CatalogCases) != 12 {
		t.Fatal("catalog source coverage incomplete")
	}
	for _, v := range f.CatalogCases {
		t.Run(v.Name, func(t *testing.T) {
			data := []byte(v.Data)
			before := string(data)
			urls, e := StoredCatalogSourceURLs(data)
			names, namesError := StoredCatalogNames(data)
			if (namesError == nil) != v.OK {
				t.Fatal("catalog name decision differs")
			}
			if (e == nil) != v.OK {
				t.Fatalf("stored catalog decision differs: %v", e)
			}
			if string(data) != before {
				t.Fatal("catalog input mutated")
			}
			if !v.OK {
				return
			}
			sort.Strings(names)
			sort.Strings(v.ExpectedNames)
			names = slices.Compact(names)
			if !reflect.DeepEqual(names, v.ExpectedNames) {
				t.Fatal("catalog name set differs")
			}
			sort.Strings(urls)
			sort.Strings(v.Expected)
			if !reflect.DeepEqual(urls, v.Expected) {
				t.Fatal("normalized catalog source multiset differs")
			}
		})
	}
}
