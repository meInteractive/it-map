package rucollation

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"sync"
)

// Data only, extracted from pinned Node24.19 / ICU78.3 / CLDR48 / Unicode17.
// See README.md and LICENSE-UNICODE. No native/runtime dependency is loaded.
//
//go:embed weights.json
var document []byte

type element [3]uint32
type weightRange struct {
	first, last         int
	primary, stride     int64
	secondary, tertiary uint32
}
type sequence struct {
	text    []int
	weights []element
}
type trie struct {
	children map[int]*trie
	weights  []element
}
type policy struct {
	ranges     []weightRange
	exceptions map[int][]element
	nfd        map[int][]int
	ccc        map[int]uint8
	root       *trie
	protected  []sequence
	reorder    map[uint32]uint32
}

var trusted = sync.OnceValue(load)

// The decompressed digest and bounded reader prevent silently using a stale,
// truncated or differently generated policy. Failure is a build artifact error.
func load() *policy {
	var doc struct {
		NodeMajor                                        int
		ICU, CLDR, Unicode, Locale, Format, SHA256, Data string
	}
	must(json.Unmarshal(document, &doc) == nil)
	must(doc.NodeMajor == 24 && doc.ICU == "78.3" && doc.CLDR == "48.0" && doc.Unicode == "17.0" && doc.Locale == "ru" && doc.Format == "icu-ru-weights-v1")
	packed, e := base64.StdEncoding.DecodeString(doc.Data)
	must(e == nil)
	z, e := gzip.NewReader(bytes.NewReader(packed))
	must(e == nil)
	data, e := io.ReadAll(io.LimitReader(z, 1026781))
	must(e == nil)
	must(z.Close() == nil)
	sum := sha256.Sum256(data)
	must(len(data) == 1026780 && hex.EncodeToString(sum[:]) == "f9fbe772bdd50e852e34e71d3fc343284aaf0a1c3459639b77cd071a66b917b9" && doc.SHA256 == hex.EncodeToString(sum[:]))
	must(string(data[:8]) == "ICURU78\x01")
	r := decoder{data: data[8:]}
	var counts [7]int
	for i := range counts {
		counts[i] = r.number(0x110000)
	}
	must(counts == [7]int{77287, 1329, 2081, 968, 1134, 1, 1})
	p := &policy{exceptions: map[int][]element{}, nfd: map[int][]int{}, ccc: map[int]uint8{}, root: &trie{}, reorder: map[uint32]uint32{}}
	previous := -1
	for i := 0; i < counts[0]; i++ {
		first := previous + 1 + r.number(0x10ffff)
		last := first + r.number(0x10ffff)
		must(last <= 0x10ffff)
		base := int64(r.number(0xffffffff))
		encoded := r.number(0x1ffffffff)
		stride := int64(encoded >> 1)
		if encoded&1 != 0 {
			stride = -stride - 1
		}
		secondary, tertiary := r.number(65535), r.number(65535)
		end := base + int64(last-first)*stride
		must(end >= 0 && end <= 0xffffffff)
		p.ranges = append(p.ranges, weightRange{first, last, base, stride, uint32(secondary), uint32(tertiary)})
		previous = last
	}
	previous = -1
	for i := 0; i < counts[1]; i++ {
		previous += 1 + r.number(0x10ffff)
		must(previous <= 0x10ffff)
		p.exceptions[previous] = r.elements()
	}
	previous = -1
	for i := 0; i < counts[2]; i++ {
		previous += 1 + r.number(0x10ffff)
		must(previous <= 0x10ffff)
		p.nfd[previous] = r.text()
	}
	previous = -1
	for i := 0; i < counts[3]; i++ {
		previous += 1 + r.number(0x10ffff)
		must(previous <= 0x10ffff)
		p.ccc[previous] = uint8(r.number(255))
	}
	for i := 0; i < counts[4]+counts[5]; i++ {
		s := sequence{r.text(), r.elements()}
		must(len(s.text) > 0)
		if i >= counts[4] {
			p.protected = append(p.protected, s)
			continue
		}
		node := p.root
		for _, cp := range s.text {
			if node.children == nil {
				node.children = map[int]*trie{}
			}
			if node.children[cp] == nil {
				node.children[cp] = &trie{}
			}
			node = node.children[cp]
		}
		must(node.weights == nil)
		node.weights = s.weights
	}
	for i := 0; i < counts[6]; i++ {
		from, to := uint32(r.number(255)), uint32(r.number(255))
		p.reorder[from] = to
	}
	must(len(r.data) == 0)
	return p
}
func must(ok bool) {
	if !ok {
		panic("invalid trusted Russian collation policy")
	}
}

type decoder struct{ data []byte }

func (r *decoder) number(max uint64) int {
	v, n := binary.Uvarint(r.data)
	must(n > 0 && v <= max)
	r.data = r.data[n:]
	return int(v)
}
func (r *decoder) text() []int {
	n := r.number(256)
	out := make([]int, n)
	for i := range out {
		out[i] = r.number(0x10ffff)
	}
	return out
}
func (r *decoder) elements() []element {
	n := r.number(256)
	out := make([]element, n)
	for i := range out {
		out[i] = element{uint32(r.number(0xffffffff)), uint32(r.number(65535)), uint32(r.number(65535))}
	}
	return out
}
