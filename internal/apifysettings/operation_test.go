package apifysettings

import (
	"errors"
	"testing"
)

func TestOperationRejectsChangedAuthorization(t *testing.T) {
	const g = "11111111-2222-4333-8444-555555555555"
	base := OperationVersion{Generation: g, SettingsRevision: 2, CompanyID: 1, SourceURL: "https://hh.ru/employer/123", SourceRevision: 3, Profile: "free"}
	settings := StoredSettings{Enabled: true, Revision: 2}
	source := HHSource{CompanyID: 1, URL: base.SourceURL, Revision: 3, Enabled: true, Supported: true}
	if err := validateOperation(base, g, settings, source); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*OperationVersion, *StoredSettings, *HHSource)
		want   error
	}{
		{"generation", func(v *OperationVersion, _ *StoredSettings, _ *HHSource) { v.Generation = "old" }, ErrGeneration},
		{"rotation", func(_ *OperationVersion, s *StoredSettings, _ *HHSource) { s.Revision++ }, ErrChanged},
		{"URL", func(_ *OperationVersion, _ *StoredSettings, s *HHSource) { s.URL = "https://hh.ru/employer/456" }, ErrChanged},
		{"owner", func(_ *OperationVersion, _ *StoredSettings, s *HHSource) { s.CompanyID++ }, ErrChanged},
		{"source revision", func(_ *OperationVersion, _ *StoredSettings, s *HHSource) { s.Revision++ }, ErrChanged},
		{"source disabled", func(_ *OperationVersion, _ *StoredSettings, s *HHSource) { s.Enabled = false }, ErrDisabled},
		{"global disabled", func(_ *OperationVersion, s *StoredSettings, _ *HHSource) { s.Enabled = false }, ErrDisabled},
		{"paid disabled", func(v *OperationVersion, _ *StoredSettings, _ *HHSource) { v.Profile = "starter" }, ErrDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, s, h := base, settings, source
			tc.change(&v, &s, &h)
			if err := validateOperation(v, g, s, h); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}
