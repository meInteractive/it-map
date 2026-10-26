package backup

import (
	"bytes"
	"encoding/json"
	"math"
)

func (v *jsonValue) set(name string, value *jsonValue) {
	for i := range v.members {
		if keyIs(v.members[i].key, name) {
			v.members[i].value = value
			return
		}
	}
	key := make([]uint16, len(name))
	for i := range name {
		key[i] = uint16(name[i])
	}
	v.members = append(v.members, jsonMember{key, value})
}

// EncodeV39 constructs manifest/integrity from the supplied snapshot document,
// validates it, and applies the exact formatted-download size cap. It preserves
// source JSON property order and scalar values. No partial file is returned.
// This is for constructing an export, not verifying an uploaded checksum.
func EncodeV39(snapshot []byte) ([]byte, error) {
	if len(snapshot) > MaxBytes {
		return nil, &InputError{Status: 413, Message: "Полный бэкап превышает 25 МБ."}
	}
	root, err := orderedJSON(snapshot)
	if err != nil {
		return nil, err
	}
	tables := root.field("storage").field("tables")
	if tables == nil || tables.kind != '[' {
		return nil, invalid("Некорректные таблицы бэкапа.")
	}
	manifest := make([]ManifestEntry, 0, len(tables.items))
	for _, t := range tables.items {
		columns, rows := t.field("columns"), t.field("rows")
		if columns == nil || columns.kind != '[' || rows == nil || rows.kind != '[' {
			return nil, invalid("Некорректные таблицы бэкапа.")
		}
		for _, r := range rows.items {
			for _, m := range r.members {
				if m.value.kind == 'f' && (math.IsInf(m.value.number, 0) || math.IsNaN(m.value.number)) {
					return nil, invalid("Некорректное числовое значение.")
				}
			}
		}
		names := make([]string, 0, len(columns.items))
		for _, c := range columns.items {
			names = append(names, textField(c, "name"))
		}
		manifest = append(manifest, ManifestEntry{textField(t, "name"), names, len(rows.items)})
	}
	if root.kind != '{' {
		return nil, invalid("Некорректный объект бэкапа.")
	}
	data, _ := json.Marshal(manifest)
	node, err := orderedJSON(data)
	if err != nil {
		return nil, err
	}
	root.set("manifest", node)
	data, _ = json.Marshal(struct {
		Algorithm string `json:"algorithm"`
		Digest    string `json:"digest"`
	}{"SHA-256", digest(root)})
	node, err = orderedJSON(data)
	if err != nil {
		return nil, err
	}
	root.set("integrity", node)
	compact := root.stringify(nil, false)
	if _, err = ValidateV39(compact); err != nil {
		return nil, err
	}
	var formatted bytes.Buffer
	if err = json.Indent(&formatted, compact, "", "  "); err != nil {
		return nil, err
	}
	if formatted.Len() > MaxBytes {
		return nil, &InputError{Status: 413, Message: "Полный бэкап превышает 25 МБ."}
	}
	return formatted.Bytes(), nil
}
