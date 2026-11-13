package dbschema

import (
	_ "embed"
	"encoding/json"
	"errors"
)

//go:embed hh_binding_v1.json
var hhBindingDocument []byte

const HHBindingCandidateVersion = 1
const hhBindingTable = "go_hh_attempt_bindings"

// HHBindingCandidateMetadata содержит metadata только таблицы связей.
// Это не денежный журнал, миграция или подтверждение установленной версии.
func HHBindingCandidateMetadata() Snapshot {
	var document struct {
		Version  int      `json:"version"`
		Metadata Snapshot `json:"metadata"`
	}
	if json.Unmarshal(hhBindingDocument, &document) != nil || document.Version != HHBindingCandidateVersion || len(document.Metadata) != 6 {
		panic("invalid HH binding metadata")
	}
	return document.Metadata
}

// CheckHHBindingCandidate проверяет точную baseline, versioned consumer и
// таблицу связей. Она не проверяет marker новой миграции, содержимое строк,
// совместимость backup/restore или разрешение на платные операции.
func CheckHHBindingCandidate(snapshot Snapshot, version int) error {
	if version != HHBindingCandidateVersion {
		return errors.New("unsupported HH binding candidate version")
	}
	trusted := HHBindingCandidateMetadata()
	if len(snapshot) != len(trusted) {
		return errors.New("incomplete HH binding metadata")
	}
	without, binding := Snapshot{}, Snapshot{}
	for section, expected := range trusted {
		rows, ok := snapshot[section]
		if !ok {
			return errors.New("incomplete HH binding metadata")
		}
		without[section] = [][]*string{}
		binding[section] = [][]*string{}
		for _, row := range rows {
			if len(row) == 0 || row[0] == nil {
				return errors.New("invalid HH binding metadata")
			}
			if *row[0] == hhBindingTable {
				binding[section] = append(binding[section], row)
			} else {
				without[section] = append(without[section], row)
			}
		}
		if !equalRows(binding[section], expected) {
			return errors.New("incompatible HH binding metadata")
		}
	}
	if err := CheckVersionedConsumerOverlay(without, ConsumerSchemaVersion); err != nil {
		return errors.New("incompatible versioned consumer metadata")
	}
	return nil
}
