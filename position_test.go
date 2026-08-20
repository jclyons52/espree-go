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

// dropRangeLoc keeps start/end (source offsets) but drops range/loc/tokens/
// comments so we can isolate node-position (start/end) fidelity.
func dropRangeLoc(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range t {
			if k == "range" || k == "loc" || k == "tokens" || k == "comments" {
				continue
			}
			out[k] = dropRangeLoc(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = dropRangeLoc(e)
		}
		return out
	default:
		return v
	}
}

func runRealEspreeFull(t *testing.T, src string) (string, string, string) {
	t.Helper()
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	esprDir := filepath.Join("original", "node_modules", "espree")
	driver := `const espree=require(process.env.ESPR_ORIG);
try{ const ast=espree.parse(process.argv[1],{sourceType:'module',ecmaVersion:'latest',tokens:true,comment:true});
 process.stdout.write(JSON.stringify(ast)); }
catch(e){ process.stdout.write('ERR:'+e.message); }`
	abs, _ := filepath.Abs(esprDir)
	cmd := exec.Command(nodeBin, "-e", driver, src)
	cmd.Env = append(os.Environ(), "ESPR_ORIG="+abs)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node failed: %v\n%s", err, out)
	}
	s := string(out)
	if len(s) >= 4 && s[:4] == "ERR:" {
		t.Skipf("real espree error: %s", s)
	}
	var ast map[string]any
	json.Unmarshal([]byte(s), &ast)
	toks, _ := json.Marshal(ast["tokens"])
	coms, _ := json.Marshal(ast["comments"])
	return s, string(toks), string(coms)
}

func TestPositionFidelity(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	for _, src := range sourceCorpus {
		js, jsToks, _ := runRealEspreeFull(t, src)
		goJSON, err := ParseJSON(src, &Options{SourceType: "module", Tokens: true, Comment: true})
		if err != nil {
			t.Errorf("go parse failed %q: %v", src, err)
			continue
		}
		var jsV, goV any
		json.Unmarshal([]byte(js), &jsV)
		json.Unmarshal([]byte(goJSON), &goV)
		if !reflect.DeepEqual(dropRangeLoc(goV), dropRangeLoc(jsV)) {
			t.Errorf("POSITION mismatch (start/end incl) for %q", src)
			firstDiff(t, dropRangeLoc(goV), dropRangeLoc(jsV), "")
			continue
		}
		var goAst map[string]any
		json.Unmarshal([]byte(goJSON), &goAst)
		if goAst["tokens"] == nil {
			t.Errorf("no tokens produced for %q", src)
			continue
		}
		goToks, _ := json.Marshal(goAst["tokens"])
		var jt, gt any
		json.Unmarshal([]byte(jsToks), &jt)
		json.Unmarshal([]byte(goToks), &gt)
		if !reflect.DeepEqual(gt, jt) {
			t.Errorf("TOKEN mismatch for %q\n go=%s\n js=%s", src, goToks, jsToks)
			continue
		}
		t.Logf("OK (pos+tokens) %q", src)
	}
}

func firstDiff(t *testing.T, a, b any, path string) {
	t.Helper()
	if reflect.DeepEqual(a, b) {
		return
	}
	switch ta := a.(type) {
	case map[string]any:
		tb := b.(map[string]any)
		for k := range ta {
			if _, ok := tb[k]; !ok {
				t.Errorf("  [%s.%s]: extra key in go", path, k)
				return
			}
		}
		for k, av := range tb {
			bv, ok := ta[k]
			if !ok {
				t.Errorf("  [%s.%s]: missing key in go", path, k)
				return
			}
			firstDiff(t, bv, av, path+"."+k)
		}
	case []any:
		tb := b.([]any)
		for i := range tb {
			if i >= len(ta) {
				t.Errorf("  [%s[%d]]: list longer in js", path, i)
				return
			}
			firstDiff(t, ta[i], tb[i], path+"["+strconv.Itoa(i)+"]")
		}
	default:
		t.Errorf("  [%s]: go=%v js=%v", path, a, b)
	}
}

func runRealEspreeTokenize(t *testing.T, src string) string {
	t.Helper()
	nodeBin, _ := exec.LookPath("node")
	esprDir := filepath.Join("original", "node_modules", "espree")
	driver := `const espree=require(process.env.ESPR_ORIG);
try{ const toks=espree.tokenize(process.argv[1],{sourceType:'module',ecmaVersion:'latest'});
 process.stdout.write(JSON.stringify(toks)); }
catch(e){ process.stdout.write('ERR:'+e.message); }`
	abs, _ := filepath.Abs(esprDir)
	cmd := exec.Command(nodeBin, "-e", driver, src)
	cmd.Env = append(os.Environ(), "ESPR_ORIG="+abs)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node failed: %v\n%s", err, out)
	}
	return string(out)
}

func TestTokenizeParity(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	for _, src := range sourceCorpus {
		js := runRealEspreeTokenize(t, src)
		if len(js) >= 4 && js[:4] == "ERR:" {
			t.Errorf("real espree.tokenize failed on %q: %s", src, js)
			continue
		}
		goToks, err := Tokenize(src, &Options{SourceType: "module"})
		if err != nil {
			t.Errorf("go tokenize failed %q: %v", src, err)
			continue
		}
		goJSON, _ := json.Marshal(goToks)
		var jt, gt any
		json.Unmarshal([]byte(js), &jt)
		json.Unmarshal(goJSON, &gt)
		if !reflect.DeepEqual(gt, jt) {
			t.Errorf("TOKENIZE mismatch for %q\n go=%s\n js=%s", src, goJSON, js)
			continue
		}
		t.Logf("OK (tokenize) %q", src)
	}
}
