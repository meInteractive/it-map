package dbschema

import (
	_ "embed"
	"encoding/json"
	"errors"
)

//go:embed consumer_version_v1.json
var consumerVersionDocument []byte

const ConsumerSchemaVersion = 1
const consumerVersionTable = "go_consumer_schema_version"

// ConsumerVersionMetadata содержит только metadata предложенной таблицы marker.
// Это кандидат, а не миграция и не доказательство установленной версии.
func ConsumerVersionMetadata() Snapshot {
	var document struct {
		Version  int      `json:"version"`
		Metadata Snapshot `json:"metadata"`
	}
	if json.Unmarshal(consumerVersionDocument, &document) != nil || document.Version != ConsumerSchemaVersion || len(document.Metadata) != 6 {
		panic("invalid consumer version metadata")
	}
	return document.Metadata
}

// CheckVersionedConsumerOverlay сверяет baseline, consumer и marker целиком.
// Значение marker и состояние singleton проверяются отдельно под row locks.
// Функция не применяет DDL и не доказывает совместимость backup или runtime.
func CheckVersionedConsumerOverlay(snapshot Snapshot, version int) error {
	if version != ConsumerSchemaVersion {
		return errors.New("unsupported consumer schema version")
	}
	trusted := ConsumerVersionMetadata()
	if len(snapshot) != len(trusted) {
		return errors.New("incomplete consumer version metadata")
	}
	without, marker := Snapshot{}, Snapshot{}
	for section, expected := range trusted {
		rows, ok := snapshot[section]
		if !ok {
			return errors.New("incomplete consumer version metadata")
		}
		without[section] = [][]*string{}
		marker[section] = [][]*string{}
		for _, row := range rows {
			if len(row) == 0 || row[0] == nil {
				return errors.New("invalid schema metadata")
			}
			if *row[0] == consumerVersionTable {
				marker[section] = append(marker[section], row)
			} else {
				without[section] = append(without[section], row)
			}
		}
		if !equalRows(marker[section], expected) {
			return errors.New("incompatible consumer version metadata")
		}
	}
	if err := CheckConsumerOverlay(without, ConsumerOverlayVersion); err != nil {
		return errors.New("incompatible consumer schema metadata")
	}
	return nil
}
