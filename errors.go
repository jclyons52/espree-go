package espree

import (
	"regexp"
	"strconv"
	"strings"

	acorn "github.com/jclyons52/acorn-go"
)

// errors.go — parse-error parity.
//
// espree overrides acorn's raise/unexpected to produce Esprima-style errors:
// the message carries no " (line:column)" suffix, lineNumber/column are
// properties (1-based line, 1-based column), and an "Unexpected token" message
// includes the offending token's source text because espree re-tokenizes at the
// failure position (espree.js unexpected()). acorn-go reports acorn's own
// format, so Parse normalizes the error through this file.

// SyntaxError is a parse error in espree's shape.
type SyntaxError struct {
	// Message is espree's err.message (no position suffix).
	Message string
	// Line is 1-based (espree's err.lineNumber).
	Line int
	// Column is 1-based (espree's err.column, which is acorn's 0-based column + 1).
	Column int
}

func (e *SyntaxError) Error() string { return e.Message }

// errorPosition matches acorn's " (line:column)" message suffix.
var errorPosition = regexp.MustCompile(`\s*\((\d+):(\d+)\)\s*$`)

// unexpectedToken matches acorn's bare "Unexpected token" message, the one
// espree re-tokenizes to name the offending token.
const unexpectedToken = "Unexpected token"

// NormalizeError converts an acorn-go parse error into espree's error shape for
// the given source text.
func NormalizeError(err error, code string) *SyntaxError {
	if err == nil {
		return nil
	}
	if se, ok := err.(*SyntaxError); ok {
		return se
	}
	msg := err.Error()
	line, col := 1, 0 // col is 0-based internally; espree reports column = col+1
	if m := errorPosition.FindStringSubmatch(msg); m != nil {
		msg = strings.TrimSpace(msg[:len(msg)-len(m[0])])
		if l, e1 := strconv.Atoi(m[1]); e1 == nil {
			line = l
		}
		if c, e2 := strconv.Atoi(m[2]); e2 == nil {
			col = c
		}
	}
	if msg == unexpectedToken {
		// espree's unexpected(): re-tokenize at the failure offset; when the
		// token there is non-empty, name it in the message. EOF (a zero-width
		// token) leaves the message bare, which is what espree does too.
		offset := newLineIndex(code).Index(line, col)
		if start, end, lexErr := acorn.LexAt(code, offset); lexErr == nil {
			if end > start && end <= len(code) {
				msg += " " + code[start:end]
			}
		} else {
			// The tokenizer rejects the position outright (e.g. "@"): espree's
			// nextToken() throws that error instead, so report it.
			if le := NormalizeError(lexErr, code); le != nil {
				return le
			}
		}
	}
	return &SyntaxError{Message: msg, Line: line, Column: col + 1}
}
