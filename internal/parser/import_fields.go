package parser

import (
	"bytes"
	"encoding/json"
)

// importFieldPlan сохраняет решение о происхождении поля. Вызывающий код
// передаёт только SQL NULL, строки и int64.
type importFieldPlan struct {
	Value            any
	Managed, Changed bool
}

func importEmpty(v any) bool { return v == nil || v == "" }

func importValueJSON(v any) string {
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(v) != nil {
		return ""
	}
	return string(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
}

func planImportField(current, incoming any, existing, manual bool, ledger string) importFieldPlan {
	managed := !existing || (!manual && (importEmpty(current) || ledger != "" && ledger == importValueJSON(current)))
	p := importFieldPlan{Value: current, Managed: managed}
	if !importEmpty(incoming) && managed && current != incoming {
		p.Value = incoming
		p.Changed = true
	}
	return p
}

// Частичное обновление не должно создавать противоречивую вилку.
func protectImportSalary(from, to importFieldPlan, oldFrom, oldTo any) (importFieldPlan, importFieldPlan) {
	a, aok := from.Value.(int64)
	b, bok := to.Value.(int64)
	if aok && bok && a > b {
		return importFieldPlan{Value: oldFrom}, importFieldPlan{Value: oldTo}
	}
	return from, to
}
