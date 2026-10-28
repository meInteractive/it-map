package backup

// Entity fields are shared by the pure legacy planner and unordered projections.
// Runtime legacy SQL/HTTP restore still requires the separate GO-100-C-S gate.
import (
	"math"
	"strings"
	"sync"
	"unicode/utf16"

	"itmap/internal/v39"
)

var catalogPolicy = sync.OnceValue(func() *jsonValue {
	v, e := orderedJSON(v39.CatalogPolicy())
	if e != nil {
		panic("invalid trusted catalog policy")
	}
	return v
})

func catalogObject() *jsonValue          { return &jsonValue{kind: '{'} }
func catalogNull() *jsonValue            { return &jsonValue{kind: 'n'} }
func catalogNumber(n float64) *jsonValue { return &jsonValue{kind: 'f', number: n} }
func catalogText(s []uint16) *jsonValue {
	return &jsonValue{kind: 's', text: append([]uint16(nil), s...)}
}
func catalogString(s string) *jsonValue { return catalogText(utf16.Encode([]rune(s))) }
func catalogLookup(v *jsonValue, key []uint16) *jsonValue {
	if v == nil || v.kind != '{' {
		return nil
	}
	for _, m := range v.members {
		if unitsKey(m.key) == unitsKey(key) {
			return m.value
		}
	}
	return nil
}
func catalogDelete(v *jsonValue, key string) {
	for i, m := range v.members {
		if keyIs(m.key, key) {
			v.members = append(v.members[:i], v.members[i+1:]...)
			return
		}
	}
}
func catalogApplicationDefault(source *jsonValue, name []uint16) (*jsonValue, bool) {
	if own := catalogLookup(source, name); own != nil {
		return own, false
	}
	switch stringFromUnits(name) {
	case "__proto__":
		return catalogObject(), false
	case "constructor", "toString", "toLocaleString", "valueOf", "hasOwnProperty", "isPrototypeOf", "propertyIsEnumerable", "__defineGetter__", "__defineSetter__", "__lookupGetter__", "__lookupSetter__":
		// Source plain-object lookup inherits a function. JSON.stringify omits
		// the property even when a non-text caller field was present initially.
		return nil, true
	}
	return nil, false
}
func catalogFinite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }
func catalogSpread(dst, src *jsonValue) {
	if src != nil {
		spreadJSON(dst, src)
	}
}
func catalogOr(v, fallback *jsonValue) *jsonValue {
	if jsonTruthy(v) {
		return v
	}
	return fallback
}
func catalogEmptyOr(v *jsonValue) *jsonValue { return catalogOr(v, catalogString("")) }
func catalogKeys(v *jsonValue) []string {
	keys := []string{}
	for _, k := range v.items {
		keys = append(keys, stringFromUnits(k.text))
	}
	return keys
}

// Math.round tests the fractional part without adding .5 (which can round an
// immediately smaller IEEE value up prematurely). Signed zero stringifies as 0.
func catalogRound(n float64) float64 {
	if !catalogFinite(n) || n == 0 {
		return n
	}
	floor := math.Floor(n)
	if n-floor < 0.5 {
		return floor
	}
	return floor + 1
}
func catalogClampRound(n, max float64) float64 { return catalogRound(math.Min(max, math.Max(0, n))) }

func finishLegacyEntity(out *jsonValue, err error) (*jsonValue, error) {
	if err != nil {
		return nil, err
	}
	// JSON.stringify applies numeric OwnPropertyKeys recursively after spread.
	// Canonicalize here so callers can store exact source JSON, not just values.
	// Overflow numbers are converted only after all source arithmetic is done.
	return orderedJSON(rawValue(out))
}

type catalogNormalizer struct{ err error }

func (n *catalogNormalizer) coercible(v *jsonValue) {
	if v == nil {
		return
	}
	if v.kind == '{' && v.field("toString") != nil {
		// JSON cannot contain callable valueOf/toString. An own toString shadows
		// Object.prototype and OrdinaryToPrimitive therefore fails in the source.
		n.err = invalid("Некорректное преобразование поля каталога.")
	}
	if v.kind == '[' {
		for _, item := range v.items {
			n.coercible(item)
		}
	}
}
func (n *catalogNormalizer) str(v *jsonValue) []uint16 { n.coercible(v); return legacyString(v) }
func (n *catalogNormalizer) num(v *jsonValue) float64  { n.coercible(v); return legacyNumber(v) }
func (n *catalogNormalizer) finite(v *jsonValue) float64 {
	f := n.num(v)
	if !catalogFinite(f) {
		return math.NaN()
	}
	return f
}
func (n *catalogNormalizer) trimmed(v *jsonValue) *jsonValue {
	if v == nil || v.kind != 's' {
		return catalogString("")
	}
	return catalogText(trimUnits(v.text))
}
func (n *catalogNormalizer) trimmedCoerced(v *jsonValue) *jsonValue {
	return catalogText(trimUnits(n.str(catalogEmptyOr(v))))
}

func (n *catalogNormalizer) sources(entity, embedded *jsonValue, keys []string, defaults *jsonValue) *jsonValue {
	out := catalogObject()
	for _, k := range keys {
		v := n.trimmed(entity.field(k))
		v = catalogOr(v, n.trimmed(embedded.field(k)))
		v = catalogOr(v, n.trimmed(defaults.field(k)))
		out.set(k, v)
	}
	return out
}
func (n *catalogNormalizer) reviews(entity, embedded *jsonValue) *jsonValue {
	return n.sources(entity.field("reviews"), embedded.field("reviews"), []string{"plusShort", "minusShort", "plus", "minus"}, nil)
}
func (n *catalogNormalizer) vacancySources(entity, embedded *jsonValue) *jsonValue {
	// Public source URL fallbacks are indexed by the exact untrimmed name.
	var defaults *jsonValue
	name := entity.field("name")
	if name != nil && name.kind == 's' {
		defaults = catalogLookup(catalogPolicy().field("vacancyDefaults"), name.text)
	}
	return n.sources(entity.field("vacancySources"), embedded.field("vacancySources"), catalogKeys(catalogPolicy().field("vacancyKeys")), defaults)
}
func (n *catalogNormalizer) salary(v *jsonValue) *jsonValue {
	out := &jsonValue{kind: '['}
	for i := 0; i < 2; i++ {
		cell := catalogNull()
		if v != nil && v.kind == '[' && i < len(v.items) {
			value := v.items[i]
			if !jsNullish(value) && !(value.kind == 's' && len(value.text) == 0) {
				f := n.num(value)
				if catalogFinite(f) && math.Trunc(f) == f && f >= 0 && f <= maxSafeInteger {
					cell = catalogNumber(f)
				}
			}
		}
		out.items = append(out.items, cell)
	}
	return out
}
func (n *catalogNormalizer) salaryPart(v *jsonValue, threshold, max, near, nearScore, upperScore float64) float64 {
	a := n.salary(v).items
	from, to := a[0], a[1]
	if from.kind != 'n' && from.number > threshold {
		return max
	}
	if from.kind != 'n' && from.number == threshold {
		return max - 1
	}
	if to.kind != 'n' && to.number > threshold {
		if from.kind != 'n' && from.number >= near {
			return nearScore
		}
		return upperScore
	}
	return 0
}
func (n *catalogNormalizer) ratingText(v *jsonValue) string {
	return stringFromUnits(roleLower(n.str(catalogEmptyOr(v))))
}
func (n *catalogNormalizer) rating(entity, embedded *jsonValue) *jsonValue {
	inputs := catalogObject()
	for _, e := range []*jsonValue{embedded, entity} {
		v := e.field("ratingInputs")
		if v != nil && (v.kind == '{' || v.kind == '[') {
			catalogSpread(inputs, v)
		}
	}
	roleFit, deferment := n.finite(inputs.field("roleFit")), n.finite(inputs.field("deferment"))
	if math.IsNaN(roleFit) {
		text := n.ratingText(entity.field("roles"))
		matches := 0
		for _, term := range []string{"product", "проект", "program", "delivery", "development", "разработ", "engineering", "техническ"} {
			if strings.Contains(text, term) {
				matches++
			}
		}
		roleFit = 2
		if matches >= 3 {
			roleFit = 5
		} else if matches >= 1 {
			roleFit = 4
		}
	} else {
		roleFit /= 4
	}
	if math.IsNaN(deferment) {
		text := n.ratingText(entity.field("deferment")) + " " + n.ratingText(entity.field("mobilization"))
		switch {
		case strings.Contains(text, "подтвержден") && strings.Contains(text, "аккредитац"):
			deferment = 15
		case strings.Contains(text, "аккредитац") && (strings.Contains(text, "вероят") || strings.Contains(text, "уточнить") || strings.Contains(text, "проверить")):
			deferment = 9
		case strings.Contains(text, "аккредитац"):
			deferment = 12
		case strings.Contains(text, "не подтвержден"):
			deferment = 3
		default:
			deferment = 5
		}
	} else {
		deferment *= 1.5
	}
	review := n.finite(entity.field("rating"))
	reviews := float64(7)
	if !math.IsNaN(review) {
		if review > 5 && review <= 10 {
			review /= 2
		}
		switch {
		case review >= 4.7:
			reviews = 20
		case review >= 4.5:
			reviews = 17
		case review >= 4.2:
			reviews = 14
		case review >= 3.9:
			reviews = 11
		case review >= 3.6:
			reviews = 8
		default:
			reviews = 5
		}
	}
	weight := catalogPolicy().field("wlbWeight").number
	formula, stored := n.num(entity.field("ratingFormulaVersion")), n.finite(entity.field("wlbScore"))
	baseline := n.finite(inputs.field("wlbScore"))
	if math.IsNaN(baseline) {
		baseline = n.finite(embedded.field("wlbScore"))
	}
	wlb := float64(0)
	switch {
	case formula >= 4 && !math.IsNaN(stored):
		wlb = catalogClampRound(stored, 100)
	case formula >= 2 && !math.IsNaN(stored):
		if !math.IsNaN(baseline) && stored == catalogClampRound(baseline*weight/15, weight) {
			wlb = catalogClampRound(baseline/15*100, 100)
		} else {
			wlb = catalogClampRound(stored/weight*100, 100)
		}
	default:
		if math.IsNaN(baseline) {
			baseline = 8
		}
		wlb = catalogClampRound(baseline/15*100, 100)
	}
	salary := n.salaryPart(entity.field("middle"), 250000, 8, 230000, 6, 3)
	senior := float64(0)
	for _, k := range []string{"senior", "lead", "head"} {
		senior = math.Max(senior, n.salaryPart(entity.field(k), 300000, 12, 280000, 9, 6))
	}
	salary += senior
	amount := n.finite(entity.field("revenue").field("amount"))
	revenue := float64(0)
	if !math.IsNaN(amount) && amount > 0 {
		revenue = 3
		for _, p := range [][2]float64{{1e12, 10}, {5e11, 9}, {1e11, 8}, {5e10, 7}, {1e10, 6}, {1e9, 5}} {
			if amount >= p[0] {
				revenue = p[1]
				break
			}
		}
	}
	evidence := n.trimmedCoerced(entity.field("aiEvidence"))
	ai := catalogNull()
	score := entity.field("aiScore")
	if !jsNullish(score) && !(score.kind == 's' && len(score.text) == 0) {
		f := n.num(score)
		if (f == 0 || f == 5 || f == 10) && (f == 0 || len(evidence.text) > 0) {
			ai = catalogNumber(f)
		}
	}
	breakdown := catalogObject()
	sum := float64(0)
	for _, p := range []struct {
		k      string
		v, max float64
	}{{"roleFit", roleFit, 5}, {"deferment", deferment, 15}, {"reviews", reviews, 20}, {"wlb", wlb / 100 * weight, weight}, {"salary", salary, 20}, {"revenue", revenue, 10}, {"ai", ai.number, 10}} {
		v := catalogClampRound(p.v, p.max)
		breakdown.set(p.k, catalogNumber(v))
		sum += v
	}
	out := catalogObject()
	out.set("score", catalogNumber(catalogClampRound(sum, 100)))
	out.set("ratingBreakdown", breakdown)
	out.set("aiScore", ai)
	out.set("aiEvidence", evidence)
	out.set("ratingInputs", inputs)
	out.set("wlbScore", catalogNumber(wlb))
	return out
}

// rank is assigned by the caller after canonical stable catalog ordering.
// These helpers only normalize one entity and never sort or plan writes.
func normalizeLegacyCompanyFields(entity *jsonValue, rank int) (*jsonValue, error) {
	out, e := normalizeCatalogCompanyObject(entity, rank)
	return finishLegacyEntity(out, e)
}

// Object builders defer stringify until their consumer finishes all source
// operations. Legacy entity callers retain their existing finished JSON view.
func normalizeCatalogCompanyObject(entity *jsonValue, rank int) (*jsonValue, error) {
	if name := entity.field("name"); name == nil || name.kind != 's' {
		return nil, invalid("Некорректное имя компании в каталоге.")
	}
	n := &catalogNormalizer{}
	var embedded *jsonValue
	for _, e := range catalogPolicy().field("embeddedCompanies").items {
		if unitsKey(e.field("name").text) == unitsKey(entity.field("name").text) {
			embedded = e
			break
		}
	}
	out := catalogObject()
	catalogSpread(out, entity)
	catalogSpread(out, n.rating(entity, embedded))
	out.set("ratingFormulaVersion", catalogPolicy().field("ratingFormulaVersion"))
	out.set("rank", catalogNull())
	for _, k := range []string{"middle", "senior", "lead", "head"} {
		out.set(k, n.salary(entity.field(k)))
	}
	out.set("vacancySources", n.vacancySources(entity, embedded))
	out.set("reviewSources", n.sources(entity.field("reviewSources"), embedded.field("reviewSources"), catalogKeys(catalogPolicy().field("reviewKeys")), nil))
	out.set("reviews", n.reviews(entity, embedded))
	for _, p := range [][2]string{{"applicationUrl", "applicationUrls"}, {"applicationEmail", "applicationEmails"}} {
		v := catalogOr(n.trimmed(entity.field(p[0])), n.trimmed(embedded.field(p[0])))
		if !jsonTruthy(v) {
			fallback, omit := catalogApplicationDefault(catalogPolicy().field(p[1]), entity.field("name").text)
			if omit {
				catalogDelete(out, p[0])
				continue
			}
			v = fallback
		}
		out.set(p[0], catalogOr(v, catalogString("")))
	}
	out.set("entityType", catalogString("company"))
	out.set("rank", catalogNumber(float64(rank)))
	return out, n.err
}

func (n *catalogNormalizer) agencyChoice(value, options *jsonValue, many bool) *jsonValue {
	values := []*jsonValue{value}
	if many {
		if value != nil && value.kind == '[' {
			values = value.items
		} else {
			values = nil
			for _, s := range strings.Split(stringFromUnits(n.str(catalogEmptyOr(value))), ";") {
				values = append(values, catalogString(s))
			}
		}
	}
	out := &jsonValue{kind: '['}
	seen := map[string]bool{}
	for _, v := range values {
		key := unitsKey(roleLower(trimUnits(n.str(catalogEmptyOr(v)))))
		for _, option := range options.items {
			if key == unitsKey(roleLower(option.text)) {
				if !many {
					return option
				}
				if !seen[key] {
					out.items = append(out.items, option)
					seen[key] = true
				}
				break
			}
		}
	}
	if !many {
		return catalogString("")
	}
	return out
}
func (n *catalogNormalizer) agencyScore(value *jsonValue, max float64, half, decimal bool) *jsonValue {
	if jsNullish(value) || len(trimUnits(n.str(value))) == 0 {
		return catalogNull()
	}
	f := n.num(value)
	if !catalogFinite(f) || f < 0 || f > max || half && math.Abs(f*2-catalogRound(f*2)) > 1e-9 {
		return catalogNull()
	}
	if half {
		return catalogNumber(catalogRound(f*2) / 2)
	}
	if decimal {
		return catalogNumber(catalogRound(f*10) / 10)
	}
	return catalogNumber(catalogRound(f))
}
func normalizeLegacyAgencyFields(entity *jsonValue) (*jsonValue, error) {
	out, e := normalizeCatalogAgencyObject(entity)
	return finishLegacyEntity(out, e)
}

func normalizeCatalogAgencyObject(entity *jsonValue) (*jsonValue, error) {
	n := &catalogNormalizer{}
	out := catalogObject()
	catalogSpread(out, entity)
	out.set("entityType", catalogString("agency"))
	rank := n.num(entity.field("rank"))
	v := catalogNull()
	if catalogFinite(rank) && rank > 0 && rank <= maxSafeInteger && math.Trunc(rank) == rank {
		v = catalogNumber(rank)
	}
	out.set("rank", v)
	for _, k := range []string{"sector", "roles"} {
		out.set(k, n.trimmed(entity.field(k)))
	}
	score := entity.field("score")
	v = catalogNull()
	if !jsNullish(score) && len(trimUnits(n.str(score))) > 0 {
		f := n.num(score)
		if catalogFinite(f) && f >= 0 && f <= 100 {
			v = catalogNumber(f)
		}
	}
	out.set("score", v)
	for _, r := range legacyAgencyRatings {
		out.set(r[0], n.agencyScore(entity.field(r[0]), 10, true, false))
	}
	for _, p := range []struct {
		k       string
		max     float64
		decimal bool
	}{{"reviewsScore", 80, true}, {"relevanceScore", 10, false}, {"revenueScore", 10, false}} {
		out.set(p.k, n.agencyScore(entity.field(p.k), p.max, false, p.decimal))
	}
	v = entity.field("confidence")
	confidence := catalogString("")
	for _, s := range []string{"Высокая", "Средняя", "Низкая"} {
		if v != nil && v.kind == 's' && stringFromUnits(v.text) == s {
			confidence = v
			break
		}
	}
	out.set("confidence", confidence)
	out.set("risk", n.trimmed(entity.field("risk")))
	for _, p := range catalogPolicy().field("options").members {
		k := stringFromUnits(p.key)
		many := k == "agencyTypes" || k == "functionalSpecializations" || k == "positionLevels" || k == "recommendedContactMethods"
		out.set(k, n.agencyChoice(entity.field(k), p.value, many))
	}
	v = n.trimmed(entity.field("workingGuide"))
	if len(v.text) > 512 {
		v = catalogText(v.text[:512])
	}
	out.set("workingGuide", v)
	salarySource := catalogObject()
	for _, k := range []string{"middle", "senior", "lead", "head"} {
		out.set(k, n.salary(entity.field(k)))
		v = entity.field("salaryRangeSource").field(k)
		s := "manual"
		if v != nil && v.kind == 's' && stringFromUnits(v.text) == "vacancies" {
			s = "vacancies"
		}
		salarySource.set(k, catalogString(s))
	}
	out.set("salaryRangeSource", salarySource)
	out.set("vacancySources", n.vacancySources(entity, nil))
	out.set("reviewSources", n.sources(entity.field("reviewSources"), nil, catalogKeys(catalogPolicy().field("agencyReviewKeys")), nil))
	out.set("reviews", n.reviews(entity, nil))
	revenue := entity.field("revenue")
	normalized := catalogObject()
	for _, k := range []string{"amount", "year"} {
		v = revenue.field(k)
		if jsNullish(v) {
			v = catalogNull()
		}
		normalized.set(k, v)
	}
	for _, k := range []string{"metric", "legal", "inn", "composition", "coverage", "status", "source1", "source2", "comment"} {
		v = revenue.field(k)
		if k == "source1" {
			v = catalogOr(v, revenue.field("source"))
		}
		normalized.set(k, n.trimmedCoerced(v))
	}
	out.set("revenue", normalized)
	for _, k := range []string{"applicationUrl", "applicationEmail"} {
		out.set(k, n.trimmed(entity.field(k)))
	}
	out.set("offices", &jsonValue{kind: '['})
	v = entity.field("jobs")
	if v == nil || v.kind != '[' {
		v = &jsonValue{kind: '['}
	}
	out.set("jobs", v)
	return out, n.err
}
