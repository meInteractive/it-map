package parser

import (
	"bytes"
	"encoding/json"
	"io"
	"itmap/internal/aiproviders"
	"math"
	"sort"
)

func ValidateLeafRoleIDs(content string, allowed []aiproviders.LeafRole) ([]int64, error) {
	var value any
	d := json.NewDecoder(bytes.NewBufferString(content))
	d.UseNumber()
	if d.Decode(&value) != nil {
		return nil, &aiproviders.ServiceError{Code: "invalid_output", Status: 422, Message: "Ответ модели не является JSON."}
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, &aiproviders.ServiceError{Code: "invalid_output", Status: 422, Message: "Ответ модели не является JSON."}
	}
	invalid := func() ([]int64, error) {
		return nil, &aiproviders.ServiceError{Code: "invalid_output", Status: 422, Message: "Модель должна выбрать только разрешённые конечные ID (не более12)."}
	}
	obj, ok := value.(map[string]any)
	if !ok || len(obj) != 1 {
		return invalid()
	}
	rows, ok := obj["role_ids"].([]any)
	if !ok || len(rows) > 12 {
		return invalid()
	}
	valid := map[int64]bool{}
	for _, r := range allowed {
		valid[r.ID] = true
	}
	seen := map[int64]bool{}
	ids := []int64{}
	for _, v := range rows {
		n, ok := v.(json.Number)
		if !ok {
			return invalid()
		}
		f, e := n.Float64()
		if e != nil || math.IsInf(f, 0) || math.IsNaN(f) || math.Trunc(f) != f || f < 1 || f > 9007199254740991 {
			return invalid()
		}
		id := int64(f)
		if !valid[id] || seen[id] {
			return invalid()
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}
