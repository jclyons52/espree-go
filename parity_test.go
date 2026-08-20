package espree

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

var sourceCorpus = []string{
	"let a = 1;",
	"const name = 'world';",
	"function add(x, y) { return x + y; }",
	"const f = (a, b) => a * b;",
	"class Foo extends Bar { constructor() { super(); } method() {} }",
	"const obj = { a: 1, b: [1, 2, 3], c: { d: true } };",
	"if (x) { y(); } else { z(); }",
	"for (let i = 0; i < 10; i++) { total += i; }",
	"for (const k in o) {} for (const v of arr) {}",
	"export default function () { return 'x'; }",
	"import a, { b as c } from 'm'; export { a };",
	"const t = `hello ${name} ${suffix}!`;",
	"try { throw new Error('e'); } catch (err) { handle(err); } finally { cleanup(); }",
	"const { x, y: [z] } = obj; const [a, ...rest] = arr;",
	"outer: for (;;) { break outer; }",
	"switch (v) { case 1: a(); break; default: b(); }",
}

func runRealEspree(t *testing.T, src string) string {
	t.Helper()
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	esprDir := filepath.Join("original", "node_modules", "espree")
	if _, err := os.Stat(esprDir); err != nil {
		t.Fatalf("vendored espree missing: %v", err)
	}
	driver := `const espree=require(process.env.ESPR_ORIG);
try{ const ast=espree.parse(process.argv[1],{sourceType:'module',ecmaVersion:'latest'}); process.stdout.write(JSON.stringify(ast)); }
catch(e){ process.stdout.write('ERR:'+e.message); }`
	abs, _ := filepath.Abs(esprDir)
	argv := []string{"-e", driver, src}
	cmd := exec.Command(nodeBin, argv...)
	cmd.Env = append(os.Environ(), "ESPR_ORIG="+abs)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node failed: %v\n%s", err, out)
	}
	return string(out)
}

// stripPos removes start/end (and range/loc) for structure-only comparison.
var ignoreKeys = map[string]bool{"start": true, "end": true, "range": true, "loc": true}

func stripPos(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range t {
			if ignoreKeys[k] {
				continue
			}
			out[k] = stripPos(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = stripPos(e)
		}
		return out
	default:
		return v
	}
}

func TestParseParity(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available; skipping espree parity")
	}
	for _, src := range sourceCorpus {
		js := runRealEspree(t, src)
		if len(js) >= 4 && js[:4] == "ERR:" {
			t.Errorf("real espree failed on %q: %s", src, js)
			continue
		}
		goJSON, err := ParseJSON(src, &Options{SourceType: "module"})
		if err != nil {
			t.Errorf("go espree failed on %q: %v", src, err)
			continue
		}
		var jsV, goV any
		if err := json.Unmarshal([]byte(js), &jsV); err != nil {
			t.Fatalf("unmarshal js: %v", err)
		}
		if err := json.Unmarshal([]byte(goJSON), &goV); err != nil {
			t.Fatalf("unmarshal go: %v", err)
		}
		if !reflect.DeepEqual(stripPos(goV), stripPos(jsV)) {
			t.Errorf("AST STRUCTURE mismatch (positions ignored) for %q", src)
			t.Errorf("  go : %s", goJSON)
			t.Errorf("  js : %s", js)
		}
	}
	t.Logf("espree parse parity OK: %d sources (structure)", len(sourceCorpus))
}

func TestParseCommentOption(t *testing.T) {
	src := "// lead\nlet a = 1; /* block */\n"
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	js := runRealEspreeComment(t, src)
	goJSON, err := ParseJSON(src, &Options{SourceType: "module", Comment: true})
	if err != nil {
		t.Fatal(err)
	}
	if stripPosJSON(goJSON) != stripPosJSON(js) {
		t.Errorf("comment option mismatch:\n  go=%s\n  js=%s", goJSON, js)
	}
	t.Logf("comment option parity OK")
}

func runRealEspreeComment(t *testing.T, src string) string {
	t.Helper()
	esprDir := filepath.Join("original", "node_modules", "espree")
	driver := `const espree=require(process.env.ESPR_ORIG);
try{ const ast=espree.parse(process.argv[1],{sourceType:'module',ecmaVersion:'latest',comment:true}); process.stdout.write(JSON.stringify(ast)); }
catch(e){ process.stdout.write('ERR:'+e.message); }`
	abs, _ := filepath.Abs(esprDir)
	argv := []string{"-e", driver, src}
	cmd := exec.Command("node", argv...)
	cmd.Env = append(os.Environ(), "ESPR_ORIG="+abs)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node failed: %v\n%s", err, out)
	}
	return string(out)
}

func stripPosJSON(s string) string {
	var v any
	_ = json.Unmarshal([]byte(s), &v)
	b, _ := json.Marshal(stripPos(v))
	return string(b)
}
