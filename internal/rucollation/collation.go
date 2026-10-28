// Package rucollation implements the accepted canonical Unicode Russian order
// for unsigned legacy catalogs. Text is never rewritten. Its only special ICU
// noncharacter key, FDD1+AC00, is preserved before decomposition (see README).
package rucollation

import "sort"

// Key holds independent primary, secondary and tertiary levels. Compute once per
// company before stable sorting; comparing keys does no normalization/allocation.
type Key [3][]uint32

func Compare(a, b Key) int {
	for level := range a {
		for i := 0; i < len(a[level]) || i < len(b[level]); i++ {
			if i == len(a[level]) {
				return -1
			}
			if i == len(b[level]) {
				return 1
			}
			if a[level][i] < b[level][i] {
				return -1
			}
			if a[level][i] > b[level][i] {
				return 1
			}
		}
	}
	return 0
}
func Keys(text []uint16) Key { return trusted().keys(text) }

func (p *policy) normalize(text []uint16) []int {
	raw := make([]int, 0, len(text))
	for i := 0; i < len(text); i++ {
		cp := int(text[i])
		if cp >= 0xd800 && cp <= 0xdbff && i+1 < len(text) && text[i+1] >= 0xdc00 && text[i+1] <= 0xdfff {
			cp = 0x10000 + (cp-0xd800)*1024 + int(text[i+1]) - 0xdc00
			i++
		}
		raw = append(raw, cp)
	}
	out := make([]int, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		cp := raw[i]
		for k, s := range p.protected {
			if len(raw)-i < len(s.text) {
				continue
			}
			match := true
			for j, w := range s.text {
				if raw[i+j] != w {
					match = false
					break
				}
			}
			if match {
				cp = -1 - k
				i += len(s.text) - 1
				break
			}
		}
		if d, ok := p.nfd[cp]; ok {
			out = append(out, d...)
		} else if cp >= 0xac00 && cp <= 0xd7a3 {
			n := cp - 0xac00
			out = append(out, 0x1100+n/588, 0x1161+(n%588)/28)
			if n%28 != 0 {
				out = append(out, 0x11a7+n%28)
			}
		} else {
			out = append(out, cp)
		}
	}
	start := 0
	for i, cp := range out {
		if p.ccc[cp] == 0 {
			p.orderMarks(out[start:i])
			start = i + 1
		}
	}
	p.orderMarks(out[start:])
	return out
}
func (p *policy) orderMarks(a []int) {
	if len(a) < 2 {
		return
	}
	if len(a) <= 32 {
		sort.SliceStable(a, func(i, j int) bool { return p.ccc[a[i]] < p.ccc[a[j]] })
		return
	}
	// Bounded counting sort prevents long adversarial combining runs from making
	// normalization quadratic; equal CCC positions retain their input order.
	var counts [256]int
	for _, cp := range a {
		counts[p.ccc[cp]]++
	}
	n := 0
	for i, c := range counts {
		counts[i] = n
		n += c
	}
	ordered := make([]int, len(a))
	for _, cp := range a {
		cc := p.ccc[cp]
		ordered[counts[cc]] = cp
		counts[cc]++
	}
	copy(a, ordered)
}
func (p *policy) single(cp int, emit func(element)) {
	if cp < 0 {
		for _, ce := range p.protected[-1-cp].weights {
			emit(ce)
		}
		return
	}
	if weights, ok := p.exceptions[cp]; ok {
		for _, ce := range weights {
			emit(ce)
		}
		return
	}
	i := sort.Search(len(p.ranges), func(i int) bool { return p.ranges[i].last >= cp })
	must(i < len(p.ranges) && p.ranges[i].first <= cp)
	r := p.ranges[i]
	emit(element{uint32(r.primary + int64(cp-r.first)*r.stride), r.secondary, r.tertiary})
}

type span struct{ start, end int }

func (p *policy) keys(text []uint16) Key {
	a := p.normalize(text)
	key := Key{}
	emit := func(ce element) {
		for level, w := range ce {
			if level == 0 {
				if lead, ok := p.reorder[w>>24]; ok {
					w = lead<<24 | w&0xffffff
				}
			}
			if w != 0 {
				key[level] = append(key[level], w)
			}
		}
	}
	// A skipped combining class blocks every later mark of that same class.
	// Jump whole runs instead of rescanning them at each contraction candidate.
	end := make([]int, len(a))
	for i := len(a) - 1; i >= 0; i-- {
		end[i] = i + 1
		if i+1 < len(a) && p.ccc[a[i]] == p.ccc[a[i+1]] {
			end[i] = end[i+1]
		}
	}
	for i := 0; i < len(a); {
		node := p.root.children[a[i]]
		var best []element
		bestEnd, bestSkipped := i+1, 0
		var skipped []span
		blocked := uint8(0)
		if node != nil {
			best = node.weights
			for j := i + 1; j < len(a) && len(node.children) > 0; {
				cc := p.ccc[a[j]]
				next := node.children[a[j]]
				if next != nil && (blocked == 0 || cc > blocked) {
					node = next
					j++
					if node.weights != nil {
						best = node.weights
						bestEnd = j
						bestSkipped = len(skipped)
					}
				} else {
					if cc == 0 {
						break
					}
					skipped = append(skipped, span{j, end[j]})
					if cc > blocked {
						blocked = cc
					}
					j = end[j]
				}
			}
		}
		if best != nil {
			for _, ce := range best {
				emit(ce)
			}
			for _, s := range skipped[:bestSkipped] {
				for _, cp := range a[s.start:s.end] {
					p.single(cp, emit)
				}
			}
			i = bestEnd
		} else {
			p.single(a[i], emit)
			i++
		}
	}
	return key
}
