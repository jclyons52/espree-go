package espree

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

// locRangeCorpus exercises the shapes that stressed loc/range derivation:
// templates (adjusted TemplateElement offsets), multi-line constructs, CRLF,
// comments, tokens, unicode separators and the empty program.
var locRangeCorpus = []string{
	"let a = 1;",
	"const t = `hello ${name} ${suffix}!`;",
	"const tagged = tag`a${1}b${2}c`;",
	"// lead\nlet a = 1; /* block */\n",
	"/* multi\n   line\n   comment */\nconst x = 1;\n",
	"\n\n  const a=1;",
	"",
	"  ",
	"function f() {\n  return {\n    a: 1,\n    b: [1, 2, 3]\n  };\n}\n",
	"const s = \"a\\n b\";\nlet t = 'x';\n",
	"class C {\n  static s = 1;\n  #priv = 2;\n  get x() { return 1; }\n}\n",
	"async function loop() {\n  for await (const x of it) { use(x); }\n}\n",
	"const re = /ab+c/giu;\nconst big = 123n;\n",
	"if (a) {\n  b();\n} else {\n  c();\n}\n",
	"const o = {\n  a: 1, // trailing\n  b: function () {}\n};\n",
	"import a, { b as c } from 'm';\nexport { a };\n",
	"const acc = items\n  .map(x => x * 2)\n  .filter(Boolean);\n",
	"switch (v) {\n  case 1:\n    a();\n    break;\n  default:\n    b();\n}\n",
	"const s2 = `line1\nline2 ${x}\nline3`;\n",
	"try { throw new Error('e'); } catch (err) { handle(err); } finally { cleanup(); }",
	"const { x, y: [z] } = obj;\nconst [a, ...rest] = arr;\n",
}

// runRealEspreeLocated parses with the ESLint option set
// (loc+range+comment+tokens) — the "full" option set ESLint always uses.
func runRealEspreeLocated(t *testing.T, src string) string {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	esprDir := filepath.Join("original", "node_modules", "espree")
	if _, err := os.Stat(esprDir); err != nil {
		t.Fatalf("vendored espree missing: %v", err)
	}
	driver := `const espree=require(process.env.ESPR_ORIG);
try{
  const ast=espree.parse(process.argv[1],{sourceType:'module',ecmaVersion:'latest',loc:true,range:true,comment:true,tokens:true});
  process.stdout.write(JSON.stringify(ast,(k,v)=>typeof v==='bigint'?v.toString():v));
}catch(e){ process.stdout.write('ERR:'+e.message); }`
	abs, _ := filepath.Abs(esprDir)
	cmd := exec.Command("node", "-e", driver, src)
	cmd.Env = append(os.Environ(), "ESPR_ORIG="+abs)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node failed: %v\n%s", err, out)
	}
	return string(out)
}

// TestLocRangeParity compares the FULL espree output — positions included —
// between espree-go and real espree for the loc+range+comment+tokens option set.
func TestLocRangeParity(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available; skipping espree loc/range parity")
	}
	checked := 0
	for _, src := range locRangeCorpus {
		js := runRealEspreeLocated(t, src)
		if len(js) >= 4 && js[:4] == "ERR:" {
			t.Errorf("real espree failed on %q: %s", src, js)
			continue
		}
		goJSON, err := ParseJSON(src, &Options{
			SourceType: "module", EcmaVersion: "latest",
			Loc: true, Range: true, Comment: true, Tokens: true,
		})
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
		if !reflect.DeepEqual(goV, jsV) {
			diff := firstPositionalDiff(goV, jsV, "$")
			t.Errorf("FULL-AST mismatch for %q\n  first diff: %s\n  go: %s\n  js: %s",
				src, diff, firstMismatchContext(goV, jsV), "")
			continue
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no cases compared")
	}
	t.Logf("LOC/RANGE PARITY PASS: %d sources, full AST+loc+range+tokens+comments identical", checked)
}

// firstPositionalDiff walks both trees and reports the first differing path.
func firstPositionalDiff(a, b any, path string) string {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return path + ": type mismatch"
		}
		for k, aVal := range av {
			bVal, exists := bv[k]
			if !exists {
				return path + "." + k + ": missing in js"
			}
			if d := firstPositionalDiff(aVal, bVal, path+"."+k); d != "" {
				return d
			}
		}
		for k := range bv {
			if _, exists := av[k]; !exists {
				return path + "." + k + ": extra in js"
			}
		}
		return ""
	case []any:
		bv, ok := b.([]any)
		if !ok || len(bv) != len(av) {
			return path + ": array length mismatch"
		}
		for i := range av {
			if d := firstPositionalDiff(av[i], bv[i], path+"["+strconv.Itoa(i)+"]"); d != "" {
				return d
			}
		}
		return ""
	default:
		if !reflect.DeepEqual(a, b) {
			return path + ": go=" + jsonScalar(a) + " js=" + jsonScalar(b)
		}
		return ""
	}
}

func firstMismatchContext(a, b any) string {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if len(ab) > 600 {
		ab = ab[:600]
	}
	if len(bb) > 600 {
		bb = bb[:600]
	}
	return "go=" + string(ab) + " js=" + string(bb)
}

func jsonScalar(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
