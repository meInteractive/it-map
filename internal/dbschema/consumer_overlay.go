package dbschema

import (
	_ "embed"
	"encoding/json"
	"errors"
)

//go:embed consumer_overlay_v1.json
var consumerOverlayDocument []byte

const ConsumerOverlayVersion = 1
const consumerControlTable = "go_consumer_control"

// ConsumerOverlayMetadata возвращает независимую копию проверяемого кандидата.
// Это не миграция и не доказательство версии применённой схемы или singleton.
func ConsumerOverlayMetadata() Snapshot {
	var document struct {
		Version  int      `json:"version"`
		Metadata Snapshot `json:"metadata"`
	}
	if json.Unmarshal(consumerOverlayDocument, &document) != nil || document.Version != ConsumerOverlayVersion || len(document.Metadata) != 6 {
		panic("invalid consumer schema evidence")
	}
	return document.Metadata
}

// CheckConsumerOverlay проверяет полный снимок: неизменённую trusted baseline
// и точные метаданные таблицы consumer. Он не выполняет DDL, не проверяет
// singleton или версию миграции, не разрешает backup/restore и не подключён к
// runtime. Версия является выбранной вызывающим политикой, не доказательством
// записанного migration marker. Неизвестные объекты и ссылки отвергаются.
func CheckConsumerOverlay(snapshot Snapshot, version int) error {
	if version != ConsumerOverlayVersion {
		return errors.New("unsupported consumer schema version")
	}
	trusted := ConsumerOverlayMetadata()
	if len(snapshot) != len(trusted) {
		return errors.New("incomplete consumer schema metadata")
	}
	baseline, overlay := Snapshot{}, Snapshot{}
	for section, expected := range trusted {
		rows, ok := snapshot[section]
		if !ok {
			return errors.New("incomplete consumer schema metadata")
		}
		baseline[section] = [][]*string{}
		overlay[section] = [][]*string{}
		for _, row := range rows {
			if len(row) == 0 || row[0] == nil {
				return errors.New("invalid schema metadata")
			}
			if *row[0] == consumerControlTable {
				overlay[section] = append(overlay[section], row)
			} else {
				baseline[section] = append(baseline[section], row)
			}
		}
		if !equalRows(overlay[section], expected) {
			return errors.New("incompatible consumer schema metadata")
		}
	}
	if err := CheckFull(baseline); err != nil {
		return errors.New("incompatible baseline schema metadata")
	}
	return nil
}
