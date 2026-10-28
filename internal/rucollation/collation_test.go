package rucollation

import (
	"reflect"
	"testing"
	"unicode/utf16"
)

func units(s string) []uint16 { return utf16.Encode([]rune(s)) }
func TestCanonicalRussianOrder(t *testing.T) {
	for _, pair := range [][2]string{{"и\U0001e4ec\u0308", "и\u0308\U0001e4ec"}, {"Й", "И\u0306"}, {"é", "e\u0301"}, {"가", "가"}, {"\u1e69", "s\u0323\u0307"}} {
		if Compare(Keys(units(pair[0])), Keys(units(pair[1]))) != 0 {
			t.Fatalf("canonical key differs: %q", pair)
		}
	}
	for _, pair := range [][2]string{{"а", "б"}, {"я", "a"}, {"a", "b"}, {"a", "A"}, {"2", "20"}} {
		if Compare(Keys(units(pair[0])), Keys(units(pair[1]))) >= 0 {
			t.Fatalf("Russian order differs: %q", pair)
		}
	}
	// Lone UTF16 is preserved, not collapsed into U+FFFD or its neighboring unit.
	if Compare(Keys([]uint16{0xd800}), Keys([]uint16{0xd801})) >= 0 {
		t.Fatal("lone UTF16 lost")
	}
	key := Keys(units("CI Пример"))
	before := Keys(units("CI Пример"))
	key[0][0]++
	if !reflect.DeepEqual(Keys(units("CI Пример")), before) {
		t.Fatal("caller changed trusted table")
	}
}
func TestCombiningRuns(t *testing.T) {
	p := trusted()
	// A non-starter contraction prefix repeated in an untrusted name used to
	// rescan the entire remaining combining run at every input position.
	exercised := false
	for cp, node := range p.root.children {
		if p.ccc[cp] == 0 || len(node.children) == 0 {
			continue
		}
		exercised = true
		one := Keys(units(string(rune(cp))))
		raw := make([]uint16, 0, 100000)
		for i := 0; i < 50000; i++ {
			raw = append(raw, units(string(rune(cp)))...)
		}
		got := Keys(raw)
		for level := range got {
			if len(got[level]) != len(one[level])*50000 {
				t.Fatal("combining run lost weights")
			}
		}
	}
	if !exercised {
		t.Fatal("missing non-starter contraction run coverage")
	}
	a, b := units("и"), units("и")
	for i := 0; i < 50000; i++ {
		a = append(a, 0x315, 0x300)
		b = append(b, 0x300)
	}
	for i := 0; i < 50000; i++ {
		b = append(b, 0x315)
	}
	if Compare(Keys(a), Keys(b)) != 0 {
		t.Fatal("long canonical ordering lost")
	}
}
