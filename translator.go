package espree

import (
	acorn "github.com/jclyons52/acorn-go"
)

// convertTokens translates acorn-go's raw tokens into espree's esprima-style
// tokens for program.tokens (token-translator.js). Only emits the subset
// needed for non-JSX code; regex values are approximated as the raw source.
func convertTokens(tokens []acorn.Token, code string) []map[string]any {
	var out []map[string]any
	var curly *acorn.Token
	var buf []acorn.Token

	push := func(tok acorn.Token) { out = append(out, translateToken(tok, code)) }

	for i := range tokens {
		tk := tokens[i]
		switch tk.Label {
		case "eof":
			if curly != nil {
				push(*curly)
				curly = nil
			}
		case "`":
			if curly != nil {
				push(*curly)
				curly = nil
			}
			buf = append(buf, tk)
			if len(buf) > 1 {
				out = append(out, templatePart(buf, code))
				buf = nil
			}
		case "${":
			buf = append(buf, tk)
			out = append(out, templatePart(buf, code))
			buf = nil
		case "}":
			if curly != nil {
				push(*curly)
			}
			curly = &tk
		case "template", "invalidTemplate":
			if curly != nil {
				buf = append(buf, *curly)
				curly = nil
			}
			buf = append(buf, tk)
		default:
			if curly != nil {
				push(*curly)
				curly = nil
			}
			push(tk)
		}
	}
	return out
}

// templatePart converts a buffered template token group into one Template
// token whose value spans from the first token's start to the last's end.
// Matches espree with range:false: template tokens carry type+value only
// (start/end are only added when range:true, which we don't emit yet).
func templatePart(buf []acorn.Token, code string) map[string]any {
	first, last := buf[0], buf[len(buf)-1]
	return map[string]any{
		"type":  "Template",
		"value": code[first.Start:last.End],
	}
}

// translateToken maps one raw acorn-go token to an esprima token.
func translateToken(tok acorn.Token, code string) map[string]any {
	label := tok.Label
	raw := code[tok.Start:tok.End]
	t := map[string]any{}

	switch {
	case label == "name":
		t["type"] = "Identifier"
		if raw == "static" || raw == "yield" || raw == "let" {
			t["type"] = "Keyword"
		}
	case label == "privateId":
		t["type"] = "PrivateIdentifier"
	case punctuatorLabel(label):
		t["type"] = "Punctuator"
	case label == "num":
		t["type"] = "Numeric"
	case label == "string":
		t["type"] = "String"
	case label == "regexp":
		t["type"] = "RegularExpression"
		// espree adds token.regex = {pattern, flags}; derive from the raw
		// "/pattern/flags" (acorn-go doesn't yet expose them separately).
		if pat, flags, ok := splitRegex(raw); ok {
			t["regex"] = map[string]any{"pattern": pat, "flags": flags}
		}
	case keywordValue(label):
		switch label {
		case "true", "false":
			t["type"] = "Boolean"
		case "null":
			t["type"] = "Null"
		default:
			t["type"] = "Keyword"
		}
	default:
		t["type"] = "Punctuator"
	}

	// For a regexp the value is "/pattern/flags"; we approximate with the raw
	// source (acorn-go doesn't yet expose pattern/flags separately).
	t["value"] = raw
	// espree strips the leading '#' from PrivateIdentifier values (acorn's
	// privateId token.value is the bare name; acorn-go keeps the raw "#name").
	if label == "privateId" && len(raw) > 0 && raw[0] == '#' {
		t["value"] = raw[1:]
	}
	t["start"] = float64(tok.Start)
	t["end"] = float64(tok.End)
	return t
}

// splitRegex splits a regex literal raw ("/pattern/flags") into pattern and
// flags by finding the last unescaped '/'. Escaped '/' (\\) inside the pattern
// are skipped so "/a\/b/g" yields pattern "a\\/b", flags "g".
func splitRegex(raw string) (pattern, flags string, ok bool) {
	if len(raw) < 3 || raw[0] != '/' {
		return "", "", false
	}
	last := -1
	for i := 1; i < len(raw); i++ {
		if raw[i] == '\\' {
			i++ // skip escaped char
			continue
		}
		if raw[i] == '/' {
			last = i
		}
	}
	if last < 0 {
		return "", "", false
	}
	return raw[1:last], raw[last+1:], true
}

// punctuatorLabel reports whether a token label is a punctuator in espree's
// translator (semi/comma/parens/braces/dot/brackets/colon/question/ellipsis/
// arrow/incDec/starstar/prefix/questionDot/binops/isAssign).
func punctuatorLabel(l string) bool {
	switch l {
	case ";", ",", "(", ")", "{", "}", ".", "[", "]", ":", "?", "...", "=>",
		"++/--", "**", "!/~", "?.",
		"||", "&&", "|", "^", "&", "==/!=/===/!==", "</>/<=/>=", "<</>>/>>>",
		"+/-", "%", "*", "/", "??", "=", "_=":
		return true
	}
	return false
}

// keywordValue reports whether a label is a JS keyword token (acorn-go labels
// keywords by their word).
func keywordValue(l string) bool {
	switch l {
	case "break", "case", "catch", "continue", "debugger", "default", "do",
		"else", "finally", "for", "function", "if", "return", "switch",
		"throw", "try", "var", "while", "with", "new", "this", "super",
		"class", "extends", "export", "import", "null", "true", "false",
		"in", "instanceof", "typeof", "void", "delete", "const":
		return true
	}
	return false
}
