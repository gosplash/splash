package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/toolschema"
)

func writeSplashFile(t *testing.T, dir, name, src string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	runErr := fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}

	return buf.String(), runErr
}

func TestCommandsRejectImportedRedline(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeSplashFile(t, dir, "main.splash", `
module main
use lib

fn run_agent() needs Agent -> String { return dangerous() }
fn main() { println(run_agent()) }
`)
	writeSplashFile(t, dir, "lib.splash", `
module lib

redline fn dangerous() -> String { return "boom" }
`)

	binary := filepath.Join(dir, "program")
	for _, command := range []struct {
		name string
		run  func() error
	}{
		{"check", func() error { return runCheck(mainPath) }},
		{"emit", func() error { return runEmit(mainPath) }},
		{"build", func() error { return runBuild(mainPath, binary) }},
		{"tools", func() error { return runTools(mainPath, toolschema.FormatOpenAI) }},
	} {
		t.Run(command.name, func(t *testing.T) {
			out, err := captureStdout(t, command.run)
			if err == nil || out != "" {
				t.Fatalf("expected rejection without output; got error %v and stdout %q", err, out)
			}
		})
	}
	if _, err := os.Stat(binary); !os.IsNotExist(err) {
		t.Fatalf("expected no binary on rejection; got stat error %v", err)
	}
}

func TestAnalyzeProgramRetainsTypedModulesAndSource(t *testing.T) {
	dir := t.TempDir()
	entry := writeSplashFile(t, dir, "main.splash", `
module main
use lib as local
fn main() { println(local.answer()) }
`)
	const original = "module lib\nfn answer() -> Int { return 42 }\n"
	lib := writeSplashFile(t, dir, "lib.splash", original)

	program, err := analyzeProgram(entry)
	if err != nil {
		t.Fatal(err)
	}
	if len(program.modules) != 2 {
		t.Fatalf("expected one imported module and one entry module, got %d", len(program.modules))
	}
	imported, root := program.modules[0], program.modules[1]
	if imported.File.Module.Name != "lib" || root.File.Module.Name != "main" {
		t.Fatal("expected imports before the entry module, with original module ownership")
	}
	fn := imported.File.Declarations[0].(*ast.FunctionDecl)
	value := fn.Body.Stmts[0].(*ast.ReturnStmt).Value
	if typ := imported.Types[value]; typ == nil || typ.TypeName() != "Int" {
		t.Fatalf("expected the imported return expression to retain type Int, got %v", typ)
	}
	writeSplashFile(t, dir, "lib.splash", "module lib\nfn answer() -> Int { return 99 }\n")
	if program.sources[lib] != original || !strings.Contains(program.sources[entry], "use lib as local") {
		t.Fatal("expected captured source from both modules, including the original imported bytes")
	}
}

func TestRunEmit_ImportedApproveWidensCallers(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeSplashFile(t, dir, "main.splash", `
module main
use lib

fn run_agent() -> String { return charge() }
`)
	writeSplashFile(t, dir, "lib.splash", `
module lib

approve fn charge() -> String { return "ok" }
`)

	out, err := captureStdout(t, func() error { return runEmit(mainPath) })
	if err != nil {
		t.Fatalf("runEmit failed: %v", err)
	}
	if !strings.Contains(out, "func run_agent() (string, error)") {
		t.Fatalf("expected caller signature to widen for imported approve fn, got:\n%s", out)
	}
	if !strings.Contains(out, "return charge()") {
		t.Fatalf("expected imported approve fn return call to propagate error tuple, got:\n%s", out)
	}
	if strings.Contains(out, "return charge(), nil") {
		t.Fatalf("expected direct return to avoid double-wrapping imported approve fn result, got:\n%s", out)
	}
}

func TestRunTools_IncludesImportedTools(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeSplashFile(t, dir, "main.splash", `
module main
use lib

fn run_agent() needs Agent -> String { return imported_tool("x") }
`)
	writeSplashFile(t, dir, "lib.splash", `
module lib

/// A tool defined in an imported module.
tool fn imported_tool(query: String) -> String { return query }
`)

	out, err := captureStdout(t, func() error {
		return runTools(mainPath, toolschema.FormatOpenAI)
	})
	if err != nil {
		t.Fatalf("runTools failed: %v", err)
	}
	if !strings.Contains(out, "\"name\": \"imported_tool\"") {
		t.Fatalf("expected imported tool in schema output, got:\n%s", out)
	}
}

func TestRunGraph_ShowsAgentRootsAndCalls(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeSplashFile(t, dir, "main.splash", `
module main

tool fn search(query: String) needs DB.read -> String { return query }

fn run_agent(query: String) needs Agent, DB.read -> String {
    return search(query)
}
`)

	out, err := captureStdout(t, func() error { return runGraph(mainPath) })
	if err != nil {
		t.Fatalf("runGraph failed: %v", err)
	}
	for _, want := range []string{
		"agent roots:",
		"- run_agent",
		"- search",
		"- run_agent [effects: DB.read|Agent] [agent]",
		"calls:",
		"- search",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in graph output, got:\n%s", want, out)
		}
	}
}

func TestRunEffects_ListsFunctionEffects(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeSplashFile(t, dir, "main.splash", `
module main

fn fetch() needs DB.read -> String { return "ok" }
fn run_agent() needs Agent, DB.read, AI -> String { return fetch() }
`)

	out, err := captureStdout(t, func() error { return runEffects(mainPath) })
	if err != nil {
		t.Fatalf("runEffects failed: %v", err)
	}
	for _, want := range []string{
		"effects:",
		"- fetch: DB.read",
		"- run_agent: DB.read|AI|Agent",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in effects output, got:\n%s", want, out)
		}
	}
}

func TestRunApprovals_ShowsApproveFunctionsAndCallers(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeSplashFile(t, dir, "main.splash", `
module main

approve fn charge() needs Net -> String { return "ok" }

fn bill_customer() needs Net -> String { return charge() }
`)

	out, err := captureStdout(t, func() error { return runApprovals(mainPath) })
	if err != nil {
		t.Fatalf("runApprovals failed: %v", err)
	}
	for _, want := range []string{
		"approval-gated functions:",
		"- charge",
		"approval callers:",
		"- bill_customer",
		"- charge",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in approvals output, got:\n%s", want, out)
		}
	}
}
