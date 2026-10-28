package backup

import "encoding/json"

// PrepareCatalogSearch preserves source String/coercion/lower/includes before
// final JSON stringify. queryJSON must be a JSON string, retaining UTF16 units.
// It searches only companies: the legacy agencies/search route is an alias.
func PrepareCatalogSearch(data []byte, queryJSON json.RawMessage) (json.RawMessage, error) {
	root, e := storedReadCatalog(data)
	if e != nil {
		return nil, e
	}
	query, e := orderedJSON(queryJSON)
	if e != nil || query.kind != 's' {
		return nil, ErrCatalogRead
	}
	q := roleLower(query.text)
	prefix := catalogSearchPrefix(q)
	out := &jsonValue{kind: '[', items: []*jsonValue{}}
	for _, c := range root.field("companies").items {
		n := &catalogNormalizer{}
		parts := [][]uint16{n.str(c.field("name")), n.str(c.field("sector")), n.str(c.field("roles"))}
		offices, e := readArray(c.field("offices"))
		if e != nil {
			return nil, e
		}
		officeText := [][]uint16{}
		for _, o := range offices {
			if o == nil || o.kind == 'n' {
				return nil, ErrCatalogRead
			}
			text := append(append([]uint16{}, n.str(o.field("metro"))...), ' ')
			text = append(text, n.str(o.field("address"))...)
			officeText = append(officeText, text)
		}
		parts = append(parts, joinCatalogSearch(officeText))
		jobs, e := readArray(c.field("jobs"))
		if e != nil {
			return nil, e
		}
		titles := []*jsonValue{}
		for _, j := range jobs {
			if j == nil || j.kind == 'n' {
				return nil, ErrCatalogRead
			}
			titles = append(titles, j.field("title"))
		}
		parts = append(parts, joinCatalogSearchValues(titles, n))
		for _, field := range []string{"legal", "metric"} {
			parts = append(parts, n.str(catalogEmptyOr(c.field("revenue").field(field))))
		}
		parts = append(parts, joinCatalogSearchValues(catalogSearchValues(c.field("vpnAi")), n))
		if n.err != nil {
			return nil, n.err
		}
		if catalogSearchIncludes(roleLower(joinCatalogSearch(parts)), q, prefix) {
			row := catalogObject()
			readMember(row, c, "name")
			readMember(row, c, "sector")
			out.items = append(out.items, row)
		}
	}
	return rawValue(out), nil
}
func joinCatalogSearch(parts [][]uint16) []uint16 {
	out := []uint16{}
	for i, p := range parts {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, p...)
	}
	return out
}
func joinCatalogSearchValues(values []*jsonValue, n *catalogNormalizer) []uint16 {
	parts := make([][]uint16, 0, len(values))
	for _, v := range values {
		var text []uint16
		if !jsNullish(v) {
			text = n.str(v)
		}
		parts = append(parts, text)
	}
	return joinCatalogSearch(parts)
}
func catalogSearchValues(v *jsonValue) []*jsonValue {
	if !jsonTruthy(v) {
		return nil
	}
	switch v.kind {
	case '[':
		return v.items
	case '{':
		out := make([]*jsonValue, 0, len(v.members))
		for _, m := range v.members {
			out = append(out, m.value)
		}
		return out
	case 's':
		out := make([]*jsonValue, 0, len(v.text))
		for _, u := range v.text {
			out = append(out, &jsonValue{kind: 's', text: []uint16{u}})
		}
		return out
	}
	return nil
}

// Linear UTF16-unit substring matching, including lone surrogates. Matching
// encoded byte strings could incorrectly find a half-unit boundary.
func catalogSearchPrefix(q []uint16) []int {
	p := make([]int, len(q))
	for i, j := 1, 0; i < len(q); i++ {
		for j > 0 && q[i] != q[j] {
			j = p[j-1]
		}
		if q[i] == q[j] {
			j++
		}
		p[i] = j
	}
	return p
}
func catalogSearchIncludes(text, q []uint16, prefix []int) bool {
	if len(q) == 0 {
		return true
	}
	j := 0
	for _, u := range text {
		for j > 0 && u != q[j] {
			j = prefix[j-1]
		}
		if u == q[j] {
			j++
		}
		if j == len(q) {
			return true
		}
	}
	return false
}
