package espree

// loc.go — loc/range emission.
//
// acorn-go produces nodes with numeric `start`/`end` offsets only. espree (and
// therefore ESLint) is always invoked with {loc:true, range:true}, so every
// node, token and comment carries a {line,column} location object and a
// [start,end] range array. This file synthesizes both from the offsets, using
// the same line-breaking rules acorn uses (LF, CRLF, lone CR, U+2028, U+2029).
//
// It runs AFTER espree's node adjustments (Program bounds, TemplateElement
// backtick offsets) so the derived loc reflects the adjusted offsets — which is
// what real espree reports.

// lineIndex maps byte offsets to 1-based lines / 0-based columns.
type lineIndex struct {
	starts []int // starts[i] = byte offset where 0-based line i begins
}

func newLineIndex(code string) *lineIndex {
	starts := []int{0}
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c == '\n':
			starts = append(starts, i+1)
		case c == '\r':
			if i+1 < len(code) && code[i+1] == '\n' {
				i++ // CRLF is a single break; the LF branch handles the append
				starts = append(starts, i+1)
			} else {
				starts = append(starts, i+1)
			}
		case c == 0xE2 && i+2 < len(code) && code[i+1] == 0x80 &&
			(code[i+2] == 0xA8 || code[i+2] == 0xA9):
			// U+2028 / U+2029 (line / paragraph separator)
			i += 2
			starts = append(starts, i+1)
		}
	}
	return &lineIndex{starts: starts}
}

// pos converts a byte offset into espree's {line, column} object.
func (li *lineIndex) pos(off int) map[string]any {
	if off < 0 {
		off = 0
	}
	// Binary search for the last start <= off.
	lo, hi := 0, len(li.starts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if li.starts[mid] <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return map[string]any{
		"line":   float64(lo + 1),
		"column": float64(off - li.starts[lo]),
	}
}

// locOf builds the {start,end} location object for an offset pair.
func (li *lineIndex) locOf(start, end int) map[string]any {
	return map[string]any{"start": li.pos(start), "end": li.pos(end)}
}

// applyLocRange walks the parsed program adding loc and/or range to every
// node, token and comment that carries numeric start/end offsets.
func applyLocRange(prog map[string]any, code string, withLoc, withRange bool) {
	if !withLoc && !withRange {
		return
	}
	li := newLineIndex(code)
	for _, key := range []string{"body", "tokens", "comments"} {
		switch v := prog[key].(type) {
		case []any:
			for _, el := range v {
				if m, ok := el.(map[string]any); ok {
					annotate(m, li, withLoc, withRange)
				}
			}
		case []map[string]any:
			for _, m := range v {
				annotate(m, li, withLoc, withRange)
			}
		}
	}
	annotate(prog, li, withLoc, withRange)
}

// annotate sets loc/range on n and recurses into child nodes/arrays.
func annotate(n map[string]any, li *lineIndex, withLoc, withRange bool) {
	if n == nil {
		return
	}
	typ, _ := n["type"].(string)
	if typ != "" {
		start, sok := offset(n["start"])
		end, eok := offset(n["end"])
		if sok && eok {
			if withLoc {
				if _, exists := n["loc"]; !exists {
					n["loc"] = li.locOf(start, end)
				}
			}
			if withRange {
				if _, exists := n["range"]; !exists {
					n["range"] = []any{float64(start), float64(end)}
				}
			}
		}
	}
	for _, v := range n {
		switch t := v.(type) {
		case map[string]any:
			if _, ok := t["type"].(string); ok {
				annotate(t, li, withLoc, withRange)
			}
		case []any:
			for _, el := range t {
				if m, ok := el.(map[string]any); ok {
					if _, ok := m["type"].(string); ok {
						annotate(m, li, withLoc, withRange)
					}
				}
			}
		}
	}
}

// offset coerces a JSON-decoded numeric offset.
func offset(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	}
	return 0, false
}
