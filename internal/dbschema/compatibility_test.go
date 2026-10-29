package dbschema

import "testing"

func TestLegacyIndexDirectionsPreserveSchemaChecks(t *testing.T) {
	metadata := Trusted().Metadata
	descending := 0
	for _, row := range metadata["indexes"] {
		if cell(row, 5) == "D" {
			ascending := "A"
			row[5] = &ascending
			descending++
		}
	}
	if descending != 15 {
		t.Fatal("trusted DESC fixture changed")
	}
	if CheckFull(metadata) == nil {
		t.Fatal("modern server accepted changed index directions")
	}
	normalizeLegacyIndexDirections(metadata)
	if CheckFull(metadata) != nil {
		t.Fatal("legacy server directions were not normalized")
	}
	// Приведение направления не должно скрывать изменение уникальности индекса.
	changed := "9"
	metadata["indexes"][0][2] = &changed
	normalizeLegacyIndexDirections(metadata)
	if CheckFull(metadata) == nil {
		t.Fatal("legacy normalization hid index corruption")
	}
}

func TestMariaDBFeatureVersionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		version             string
		ignored, descending bool
	}{
		{"10.3.32-MariaDB", false, false},
		{"10.5.29-MariaDB", false, false},
		{"10.6.0-MariaDB", true, false},
		{"10.7.8-MariaDB", true, false},
		{"10.8.0-MariaDB", true, true},
		{"11.4.8-MariaDB", true, true},
	} {
		if mariaAtLeast(tc.version, 10, 6, 0) != tc.ignored || mariaAtLeast(tc.version, 10, 8, 0) != tc.descending {
			t.Fatalf("incorrect metadata capabilities for %s", tc.version)
		}
	}
}
