package parser

import (
	"bytes"
	"encoding/json"
)

// importFieldPlan mirrors the legacy import's ownership check. Values are
// limited to database NULL, strings and int64 by the caller.
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

// A partially managed range must never become invalid by updating one end.
func protectImportSalary(from, to importFieldPlan, oldFrom, oldTo any) (importFieldPlan, importFieldPlan) {
	a, aok := from.Value.(int64)
	b, bok := to.Value.(int64)
	if aok && bok && a > b {
		return importFieldPlan{Value: oldFrom}, importFieldPlan{Value: oldTo}
	}
	return from, to
}
