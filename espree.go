// Package espree is a Go port of the npm package espree (v9.6.1) — the ESLint
// parser (ESLint 8.x's graph). It wraps the parity-verified acorn-go parser
// and applies espree's parse() post-processing: program.sourceType, Program
// bounds adjustment (start = first node's start), comment attachment, and
// per-node fixes (TemplateElement backtick offsets, Function.generator). Nodes
// are the ESTree map[string]any form acorn-go produces.
//
// The vendored original (with its deps, for the parity oracle) lives in
// original/.
//
// SCOPE NOTES: currently supports sourceType "module" + ecmaVersion "latest"
// (acorn-go is module-only). script/commonjs sourceType, older ecmaVersions,
// ranges/loc emission, and tokenize()/tokens are tracked as follow-up pieces.
package espree

import (
	"encoding/json"
	"strings"

	acorn "github.com/jclyons52/acorn-go"
)

// Options mirrors espree's parser options. Range/Loc map to ESLint "range"/"loc".
type Options struct {
	SourceType  string
	EcmaVersion any
	Range       bool
	Loc         bool
	Comment     bool
	Tokens      bool
}

type state struct {
	originalSourceType string
	// acornSourceType is what the parser is told: espree maps its
	// "commonjs" sourceType onto acorn's "script".
	acornSourceType string
	comment         bool
	tokens          bool
	rangeAllowed    bool
	locAllowed      bool
}

func normalize(opts *Options) *state {
	s := &state{originalSourceType: "module", acornSourceType: "module"}
	if opts == nil {
		return s
	}
	if opts.SourceType == "script" || opts.SourceType == "commonjs" {
		s.originalSourceType = opts.SourceType
		s.acornSourceType = "script"
	} else {
		s.originalSourceType = "module"
	}
	s.comment = opts.Comment
	s.tokens = opts.Tokens
	s.rangeAllowed = opts.Range
	s.locAllowed = opts.Loc
	return s
}

// Parse parses code and returns the espree-normalized ESTree AST (JS
// espree.parse). Handles sourceType, comments, and program-bounds tweaks;
// tokens are attached only when the Tokens option is set (see translator).
func Parse(code string, opts *Options) (interface{}, error) {
	st := normalize(opts)
	base, comments, tokens, err := acorn.ParseAllWithOptions(code, acorn.ParseOptions{SourceType: st.acornSourceType})
	if err != nil {
		// espree reports Esprima-style errors: message without the position
		// suffix, 1-based line, 1-based column.
		return nil, NormalizeError(err, code)
	}
	// acorn-go's AST is an internal node struct; express it as the JSON shape
	// espree consumers expect.
	b, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	var prog map[string]any
	if err := json.Unmarshal(b, &prog); err != nil {
		return nil, err
	}
	prog["sourceType"] = st.originalSourceType

	// Program bounds, Esprima-compatible (espree.js):
	//   start = first body node's start (leading whitespace/comments excluded)
	//   end   = last real (non-EOF) token's end (trailing whitespace excluded)
	// acorn instead starts programs at 0 and counts trailing whitespace.
	body, _ := prog["body"].([]any)
	if len(body) > 0 {
		if first, ok := body[0].(map[string]any); ok {
			if s, ok := first["start"].(float64); ok {
				prog["start"] = s
			}
		}
	} else {
		prog["start"] = float64(0)
	}
	if end, ok := lastTokenEnd(tokens); ok {
		prog["end"] = end
	}

	if st.comment {
		prog["comments"] = convertComments(comments)
	}
	if st.tokens {
		prog["tokens"] = convertTokens(tokens, code, st.rangeAllowed, st.locAllowed)
	}

	adjustNodes(prog)
	// loc/range emission runs last: espree reports locations for the adjusted
	// offsets (Program bounds, TemplateElement backticks).
	applyLocRange(prog, code, opts != nil && opts.Loc, opts != nil && opts.Range)
	return prog, nil
}

// ParseJSON is a convenience returning the AST as JSON.
func ParseJSON(code string, opts *Options) (string, error) {
	v, err := Parse(code, opts)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Tokenize is the Go analogue of espree.tokenize(code, opts): it lexes the
// input and returns the esprima-style token list (via the same translator as
// Parse's tokens), without building an AST. Matches espree.tokenize's array.
func Tokenize(code string, opts *Options) ([]any, error) {
	st := normalize(opts)
	_, _, tokens, err := acorn.ParseAllWithOptions(code, acorn.ParseOptions{SourceType: st.acornSourceType})
	if err != nil {
		return nil, err
	}
	out := convertTokens(tokens, code, st.rangeAllowed, st.locAllowed)
	res := make([]any, len(out))
	for i, t := range out {
		res[i] = t
	}
	return res, nil
}

// lastTokenEnd returns the end offset of the last non-EOF acorn token, which
// is what espree's state.lastToken tracks (and uses to close the Program).
func lastTokenEnd(tokens []acorn.Token) (float64, bool) {
	for i := len(tokens) - 1; i >= 0; i-- {
		if tokens[i].Label == "eof" {
			continue
		}
		return float64(tokens[i].End), true
	}
	return 0, false
}

// convertComments maps acorn-go comments to espree's esprima-style comment
// nodes ({type, value, start, end}).
func convertComments(cs []acorn.Comment) []map[string]any {
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		typ := "Line"
		if c.Block {
			typ = "Block"
		} else if strings.HasPrefix(c.Text, "#!") {
			typ = "Hashbang"
		}
		out = append(out, map[string]any{
			"type":  typ,
			"value": c.Text,
			"start": float64(c.Start),
			"end":   float64(c.End),
		})
	}
	return out
}

// adjustNodes recursively applies espree's per-node fixes not already produced
// by acorn-go: Function.generator=false and TemplateElement backtick offsets.
func adjustNodes(n map[string]any) {
	if n == nil {
		return
	}
	typ, _ := n["type"].(string)
	switch {
	case typ == "ImportDeclaration" || typ == "ExportNamedDeclaration" || typ == "ExportAllDeclaration":
		// acorn-go emits an empty attributes:[]; acorn 8.15 omits it when empty.
		if attrs, ok := n["attributes"].([]any); ok && len(attrs) == 0 {
			delete(n, "attributes")
		}
	case typ == "ImportExpression":
		// espree's latest resolves to ecmaVersion 15, so acorn's >=16
		// `options` field on ImportExpression is never emitted by real
		// espree. Strip it (acorn-go, being true "latest", adds options:null).
		delete(n, "options")
	case typ == "Literal" && n["bigint"] == nil:
		// acorn-go stores a BigInt literal as value:raw-as-string ("123n").
		// acorn 8.x exposes value (the bigint) plus a separate bigint field
		// (the digit string). Normalize to match: bigint=digits, value=digits.
		if raw, ok := n["raw"].(string); ok && strings.HasSuffix(raw, "n") {
			digits := strings.TrimSuffix(raw, "n")
			n["bigint"] = digits
			n["value"] = digits
		}
	case strings.HasPrefix(typ, "Function"):
		if g, ok := n["generator"].(bool); !ok || !g {
			n["generator"] = false
		}
	case typ == "TemplateElement":
		adjustTemplateElement(n)
		return
	}
	for _, v := range n {
		switch t := v.(type) {
		case map[string]any:
			adjustNodes(t)
		case []any:
			for _, e := range t {
				if m, ok := e.(map[string]any); ok {
					adjustNodes(m)
				}
			}
		}
	}
}

func adjustTemplateElement(n map[string]any) {
	tail, _ := n["tail"].(bool)
	startOff, endOff := -1.0, 2.0
	if tail {
		endOff = 1.0
	}
	if s, ok := n["start"].(float64); ok {
		n["start"] = s + startOff
	}
	if e, ok := n["end"].(float64); ok {
		n["end"] = e + endOff
	}
	if r, ok := n["range"].([]any); ok && len(r) == 2 {
		if s, ok := r[0].(float64); ok {
			r[0] = s + startOff
		}
		if e, ok := r[1].(float64); ok {
			r[1] = e + endOff
		}
	}
}
