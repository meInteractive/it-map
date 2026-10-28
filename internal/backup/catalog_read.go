package backup

import (
	"encoding/json"
	"errors"
	"unicode/utf16"
)

var ErrCatalogRead = errors.New("invalid source catalog read projection")

// These rows are the already selected read set, not an alternative SQL engine.
// ID creation, duplicate-name ambiguity, winner selection and read transactions
// remain the repository's responsibility. No SQL/provider work occurs here.
type CatalogReadIdentity struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	EntityType string `json:"entityType"`
	Favorite   bool   `json:"favorite"`
	Hidden     bool   `json:"hidden"`
}
type CatalogReadApplication struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
type CatalogReadState struct {
	Identities   []CatalogReadIdentity    `json:"identities"`
	Vacancies    []string                 `json:"vacancies"`
	Applications []CatalogReadApplication `json:"applications"`
	Initialized  []string                 `json:"initialized"`
}
type CatalogReadOptions struct {
	EntityType   string `json:"entityType"`
	View         string `json:"view"`
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Favorite     bool   `json:"favorite"`
	Hidden       bool   `json:"hidden"`
	VacancyCount int64  `json:"vacancyCount"`
}

func validReadCatalog(root *jsonValue) bool {
	companies := root.field("companies")
	if companies == nil || companies.kind != '[' {
		return false
	}
	for _, c := range companies.items {
		name := c.field("name")
		if name == nil || name.kind != 's' || len(trimUnits(name.text)) == 0 {
			return false
		}
	}
	return true
}
func storedReadCatalog(data []byte) (*jsonValue, error) {
	var root *jsonValue
	if len(data) > 0 {
		var e error
		root, e = orderedJSON(data)
		if e != nil {
			return nil, ErrCatalogRead
		}
	}
	if !validReadCatalog(root) {
		root = catalogObject()
		root.set("companies", catalogPolicy().field("embeddedCompanies"))
	}
	return normalizeCatalogObject(root)
}
func readNameKey(name string) string                  { return unitsKey(utf16.Encode([]rune(name))) }
func readEntityKey(kind string, name []uint16) string { return kind + ":" + unitsKey(name) }
func readBool(b bool) *jsonValue                      { return &jsonValue{kind: 'b', boolean: b} }
func readMember(dst, source *jsonValue, key string) {
	if v := source.field(key); v != nil {
		dst.set(key, v)
	}
}
func readNullish(v *jsonValue) *jsonValue {
	if jsNullish(v) {
		return catalogNull()
	}
	return v
}
func readArray(v *jsonValue) ([]*jsonValue, error) {
	if !jsonTruthy(v) {
		return nil, nil
	}
	if v.kind != '[' {
		return nil, ErrCatalogRead
	}
	return v.items, nil
}

// PrepareCatalogRead builds the current source list/map/detail DTO without
// activating a repository/HTTP path. Names stay UTF16-exact; output stays JSON
// so lone surrogates, omitted properties and original scalar types survive.
func PrepareCatalogRead(data []byte, state CatalogReadState, options CatalogReadOptions) (json.RawMessage, error) {
	if options.EntityType == "" {
		options.EntityType = "company"
	}
	if options.View == "" {
		options.View = "list"
	}
	if options.EntityType != "company" && options.EntityType != "agency" || options.View != "list" && options.View != "map" && options.View != "detail" {
		return nil, ErrCatalogRead
	}
	root, e := storedReadCatalog(data)
	if e != nil {
		return nil, e
	}
	entities := root.field("companies")
	if options.EntityType == "agency" {
		entities = root.field("agencies")
	}
	if options.View == "detail" {
		name := options.Name
		if options.ID != 0 {
			for _, s := range state.Identities {
				if s.EntityType == options.EntityType && s.ID == options.ID && s.Name != "" {
					name = s.Name
					break
				}
			}
		}
		key := readNameKey(name)
		for _, entity := range entities.items {
			if unitsKey(entity.field("name").text) != key {
				continue
			}
			out := catalogObject()
			catalogSpread(out, entity)
			catalogDelete(out, "jobs")
			id := catalogNull()
			if options.ID != 0 {
				id = catalogNumber(float64(options.ID))
			}
			out.set("id", id)
			out.set("favorite", readBool(options.Favorite))
			out.set("hidden", readBool(options.Hidden))
			out.set("vacancyCount", catalogNumber(float64(options.VacancyCount)))
			return rawValue(out), nil
		}
		return json.RawMessage("null"), nil
	}
	identities := map[string]CatalogReadIdentity{}
	for _, s := range state.Identities {
		identities[readEntityKey(s.EntityType, utf16.Encode([]rune(s.Name)))] = s
	}
	vacancies, initialized := map[string]bool{}, map[string]bool{}
	for _, name := range state.Vacancies {
		vacancies[readNameKey(name)] = true
	}
	for _, name := range state.Initialized {
		initialized[readNameKey(name)] = true
	}
	apps := map[string]string{}
	for _, s := range state.Applications {
		apps[readNameKey(s.Name)] = s.Status
	}
	result := &jsonValue{kind: '[', items: []*jsonValue{}}
	for _, entity := range entities.items {
		name := entity.field("name")
		key := unitsKey(name.text)
		s, found := identities[readEntityKey(options.EntityType, name.text)]
		out := catalogObject()
		id := catalogNull()
		if found {
			id = catalogNumber(float64(s.ID))
		}
		out.set("id", id)
		out.set("name", name)
		readMember(out, entity, "rank")
		readMember(out, entity, "entityType")
		out.set("favorite", readBool(s.Favorite))
		out.set("hidden", readBool(s.Hidden))
		hasJobs := vacancies[key]
		if !hasJobs && !initialized[key] {
			jobs, e := readArray(entity.field("jobs"))
			if e != nil {
				return nil, e
			}
			for _, j := range jobs {
				if j == nil || j.kind == 'n' {
					return nil, ErrCatalogRead
				}
				if !jsonTruthy(j.field("title")) {
					continue
				}
				n := &catalogNormalizer{}
				url := n.str(catalogOr(j.field("url"), catalogString("")))
				if n.err != nil {
					return nil, n.err
				}
				if unitsPrefix(url, "http://") || unitsPrefix(url, "https://") {
					hasJobs = true
					break
				}
			}
		}
		out.set("hasVacancies", readBool(hasJobs))
		status, hasApp := apps[key]
		out.set("hasApplications", readBool(hasApp))
		appStatus := catalogNull()
		if status != "" {
			appStatus = catalogString(status)
		}
		out.set("applicationStatus", appStatus)
		offices, e := readArray(entity.field("offices"))
		if e != nil {
			return nil, e
		}
		projected := &jsonValue{kind: '[', items: []*jsonValue{}}
		for _, office := range offices {
			if office == nil || office.kind == 'n' {
				return nil, ErrCatalogRead
			}
			row := catalogObject()
			readMember(row, office, "address")
			readMember(row, office, "metro")
			if options.View == "map" {
				readMember(row, office, "lat")
				readMember(row, office, "lon")
			}
			projected.items = append(projected.items, row)
		}
		out.set("offices", projected)
		if options.View == "map" {
			result.items = append(result.items, out)
			continue
		}
		for _, field := range []string{"middle", "senior", "lead", "head"} {
			readMember(out, entity, field)
		}
		deferment := entity.field("deferment")
		if jsNullish(deferment) {
			deferment = catalogString("")
		}
		out.set("deferment", deferment)
		out.set("rating", readNullish(entity.field("rating")))
		out.set("wlbScore", readNullish(entity.field("wlbScore")))
		hasSource := false
		for _, m := range entity.field("vacancySources").members {
			if m.value != nil && m.value.kind == 's' && len(trimUnits(m.value.text)) > 0 {
				hasSource = true
				break
			}
		}
		out.set("hasVacancySource", readBool(hasSource))
		if options.EntityType == "agency" {
			readMember(out, entity, "reviewsScore")
			readMember(out, entity, "salaryRangeSource")
			revenue := catalogObject()
			revenue.set("amount", readNullish(entity.field("revenue").field("amount")))
			out.set("revenue", revenue)
			readMember(out, entity, "functionalSpecializations")
		}
		result.items = append(result.items, out)
	}
	return rawValue(result), nil
}
func unitsPrefix(text []uint16, prefix string) bool {
	if len(text) < len(prefix) {
		return false
	}
	for i, b := range []byte(prefix) {
		if text[i] != uint16(b) {
			return false
		}
	}
	return true
}
