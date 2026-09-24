package espree

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A shebang is the first line of essentially every npm bin script, so a parser
// that rejects it is useless on real code (this test came out of dogfooding: the
// port reported "Unexpected character '!'" where ESLint reported nothing).
//
// Two things have to be true, and both were broken:
//
//  1. acorn's allowHashBang (default on for ecmaVersion >= 14) skips the `#!`
//     line, so the file parses at all.
//  2. espree types that comment "Hashbang" by looking at the SOURCE at the
//     comment's start offset (code.slice(start, start + 2) === "#!"), not at the
//     comment text — acorn's hashbang comment text excludes the leading "#!".
//
// Positions are compared too: the shebang occupies real offsets, so any
// mis-accounting of the skipped line shows up as shifted ranges/locs.
func TestHashbangCommentAndTokenParity(t *testing.T) {
	requireOracle(t)
	srcs := []string{
		"#!/usr/bin/env node\n\"use strict\";\nlet a = 1;\n",
		"#!/usr/bin/env node\n",                       // shebang only
		"#!node\nlet a = 1;\n// trailing\n",           // no space in the shebang
		"let a = 1;\n// not a shebang: #! mid-file\n", // must stay a Line comment
		"\n#!/usr/bin/env node\nlet a = 1;\n",         // not at offset 0: not a shebang
	}
	for _, src := range srcs {
		js := runRealEspreeScriptLocated(t, src)
		goJSON, err := ParseJSON(src, &Options{
			SourceType: "script", EcmaVersion: "latest",
			Range: true, Loc: true, Comment: true, Tokens: true,
		})
		// The oracle reports a rejected source as ERR:<message>; parity means both
		// sides reject it (a shebang is only legal at byte 0, so one mid-file or
		// after a blank line is a syntax error for both).
		if strings.HasPrefix(js, "ERR:") {
			if err == nil {
				t.Errorf("%q: espree rejects it (%s) but the port parsed it", src, strings.TrimPrefix(js, "ERR:"))
			} else if !strings.Contains(err.Error(), strings.TrimPrefix(js, "ERR:")) {
				t.Errorf("%q: error text differs\n  espree: %s\n  go    : %v", src, strings.TrimPrefix(js, "ERR:"), err)
			}
			continue
		}
		if err != nil {
			t.Errorf("parse error for %q: %v", src, err)
			continue
		}
		var jsV, goV any
		if err := json.Unmarshal([]byte(js), &jsV); err != nil {
			t.Fatalf("oracle output not JSON for %q: %v\n%s", src, err, js)
		}
		if err := json.Unmarshal([]byte(goJSON), &goV); err != nil {
			t.Fatalf("go output not JSON for %q: %v", src, err)
		}
		if !reflect.DeepEqual(goV, jsV) {
			t.Errorf("hashbang parity mismatch for %q\n  go : %s\n  js : %s", src, goJSON, js)
		}
	}
	t.Logf("hashbang parity OK: %d sources (comments, tokens, ranges, locs)", len(srcs))
}

// runRealEspreeScriptLocated parses in script mode with comments, tokens, ranges
// and locations, returning the raw JSON (or ERR:... when espree throws).
func runRealEspreeScriptLocated(t *testing.T, src string) string {
	t.Helper()
	esprDir := filepath.Join("original", "node_modules", "espree")
	driver := `const espree=require(process.env.ESPR_ORIG);
try{ const ast=espree.parse(process.argv[1],{sourceType:'script',ecmaVersion:'latest',comment:true,tokens:true,range:true,loc:true}); process.stdout.write(JSON.stringify(ast)); }
catch(e){ process.stdout.write('ERR:'+e.message); }`
	abs, _ := filepath.Abs(esprDir)
	cmd := exec.Command("node", "-e", driver, src)
	cmd.Env = append(os.Environ(), "ESPR_ORIG="+abs)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node failed: %v\n%s", err, out)
	}
	return string(out)
}
