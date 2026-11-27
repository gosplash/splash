package typechecker_test

import (
	"strings"
	"testing"
)

func TestDeclaredEffectContracts(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
	}{
		{"unknown label", "fn grant() needs DB.unknown {}", `unknown effect "DB.unknown"`},
		{"unused grant still required", "fn grant() needs DB.write {}\nfn caller() { grant() }", "missing declared effect DB.write"},
		{"partial grant", "fn grant() needs DB.read, Net {}\nfn caller() needs DB.read { grant() }", "missing declared effect Net"},
		{"parent read write", "fn grant() needs DB.write {}\nfn caller() needs DB { grant() }", ""},
		{"admin remains separate", "fn grant() needs DB.admin {}\nfn caller() needs DB { grant() }", "missing declared effect DB.admin"},
		{"all documented labels", "fn grant() needs DB.read, DB.write, DB.admin, Net, AI, FS, Exec, Cache, Secrets, Queue, Metric {}\nfn caller() needs DB, DB.admin, Net, AI, FS, Exec, Cache, Secrets, Queue, Metric { grant() }", ""},
		{"structural agent", "agent fn serve() {}\nfn caller() { serve() }", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags := check("module sample\n" + tc.source + "\n")
			if tc.want == "" {
				if hasError(diags) {
					t.Fatalf("unexpected errors: %v", diags)
				}
				return
			}
			if len(diags) != 1 || !strings.Contains(diags[0].Message, tc.want) || diags[0].Pos.Line < 2 {
				t.Fatalf("want one source diagnostic containing %q, got %v", tc.want, diags)
			}
		})
	}
}

func TestDeclaredEffectContractUsesQualifiedCallee(t *testing.T) {
	const imports = "module app\nuse left as harmless\nuse right as privileged\n"
	files := map[string]string{
		"left.splash":  "module left\nfn lookup() needs DB.read {}\n",
		"right.splash": "module right\nfn lookup() needs DB.write {}\n",
	}
	if diags := checkWithImports(imports+"fn caller() needs DB.read { harmless.lookup() }\n", files); hasError(diags) {
		t.Fatalf("read grant should cover left.lookup: %v", diags)
	}
	diags := checkWithImports(imports+"fn caller() needs DB.read { privileged.lookup() }\n", files)
	if len(diags) != 1 || diags[0].Pos.Line != 4 || !strings.Contains(diags[0].Message, "missing declared effect DB.write") {
		t.Fatalf("want DB.write diagnostic at the qualified call: %v", diags)
	}
}
