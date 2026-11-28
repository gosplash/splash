package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/compiler"
	"gosplash.dev/splash/internal/ir"
	"gosplash.dev/splash/internal/safety"
	"gosplash.dev/splash/internal/vm"
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
		{name: "check", run: func() error { return runCheck(mainPath) }},
		{
			name: "run --backend vm",
			run:  func() error { return runVMCommand([]string{mainPath, "--backend", "vm"}, os.Stdout) },
		},
		{name: "emit", run: func() error { return runEmit(mainPath) }},
		{name: "build", run: func() error { return runBuild(mainPath, binary) }},
		{name: "tools", run: func() error { return runTools(mainPath, compiler.FormatOpenAI) }},
	} {
		t.Run(command.name, func(t *testing.T) {
			out, err := captureStdout(t, command.run)
			if !errors.Is(err, safety.ErrRejected) || out != "" {
				t.Fatalf("expected rejection without output; got error %v and stdout %q", err, out)
			}
		})
	}
	if _, err := os.Stat(binary); !os.IsNotExist(err) {
		t.Fatalf("expected no binary on rejection; got stat error %v", err)
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
		return runTools(mainPath, compiler.FormatOpenAI)
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

func TestResolvedNamedCallsAgreeWithGraphAndNativeOutput(t *testing.T) {
	dir := t.TempDir()
	entry := writeSplashFile(t, dir, "main.splash", `
module main
use rates as first
use shipping as second

fn total(price: Int) -> Int { return first.price() + second.price() + price }
fn main() { println(total(3)) }
`)
	writeSplashFile(t, dir, "rates.splash", `
module rates
fn price() -> Int { return 40 }
`)
	writeSplashFile(t, dir, "shipping.splash", `
module shipping
fn price() -> Int { return 2 }
`)

	program, err := analyzeProgram(entry)
	if err != nil {
		t.Fatal(err)
	}
	total := findFunction(t, program, "total")
	if got := strings.Join(total.Callees, ","); got != "rates.price,shipping.price" {
		t.Fatalf("total callees = %q; want rates.price,shipping.price", got)
	}

	executable := filepath.Join(dir, "invoice")
	if err := runBuild(entry, executable); err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := exec.Command(executable).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "45" {
		t.Fatalf("run = %q, %v; want 45", out, err)
	}
}

func TestResolvedNamedCallsRejectAmbiguousFlatImport(t *testing.T) {
	dir := t.TempDir()
	entry := writeSplashFile(t, dir, "main.splash", `
module main
use rates as first
use shipping as second

fn main() { println(price()) }
`)
	writeSplashFile(t, dir, "rates.splash", "module rates\nfn price() -> Int { return 40 }\n")
	writeSplashFile(t, dir, "shipping.splash", "module shipping\nfn price() -> Int { return 2 }\n")

	if err := runCheck(entry); err == nil {
		t.Fatal("check accepted a flat call with two distinct imported declarations")
	}
}

func TestLocalFunctionValueDoesNotResolveToShadowedDeclaration(t *testing.T) {
	entry := writeSplashFile(t, t.TempDir(), "main.splash", `
module main
fn price() -> Int { return 40 }
fn alternate() -> Int { return 2 }
fn apply(price: fn() -> Int) -> Int { return price() }
fn main() { println(apply(alternate)) }
`)
	program, err := analyzeProgram(entry)
	if err != nil {
		t.Fatal(err)
	}
	if got := findFunction(t, program, "apply").Callees; len(got) != 0 {
		t.Fatalf("indirect caller has named callees %v", got)
	}
	executable := filepath.Join(t.TempDir(), "callback")
	if err := runBuild(entry, executable); err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := exec.Command(executable).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "2" {
		t.Fatalf("run = %q, %v; want 2", out, err)
	}
}

func TestProtectedUnresolvedIndirectCallsAreIndeterminate(t *testing.T) {
	entry := writeSplashFile(t, t.TempDir(), "main.splash", `
module main
fn process(callback: fn() -> Int) needs Agent -> Int { return callback() }
`)
	program, err := analyzeProgram(entry)
	if err != nil {
		t.Fatal(err)
	}
	_, status, _ := compiler.Verify(program)
	if len(status.Violations) != 0 || len(status.Incomplete) != 1 ||
		!strings.Contains(status.Incomplete[0].Message, "unresolved call target") {
		t.Fatalf("expected a specific unresolved-target diagnostic, got %v", status)
	}
	if out, err := captureStdout(t, func() error { return runEmit(entry) }); !errors.Is(err, safety.ErrIndeterminate) || out != "" {
		t.Fatalf("emit = %q, %v; want no artifact", out, err)
	}
}

func TestQualifiedApprovalCallLowersWithErrorChannel(t *testing.T) {
	dir := t.TempDir()
	entry := writeSplashFile(t, dir, "main.splash", `
module main
use rates as pricing

fn main() {
    let price = pricing.standardPrice()
    println(price)
}
`)
	writeSplashFile(t, dir, "rates.splash", `
module rates
approve fn standardPrice() -> Int { return 42 }
`)
	executable := filepath.Join(dir, "priced")
	if err := runBuild(entry, executable); err != nil {
		t.Fatalf("build: %v", err)
	}
	cmd := exec.Command(executable)
	cmd.Stdin = strings.NewReader("y\n")
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != "42" {
		t.Fatalf("run = %q, %v; want 42", out, err)
	}
}

func TestQualifiedNamedCallsApplyExistingPolicy(t *testing.T) {
	dir := t.TempDir()
	entry := writeSplashFile(t, dir, "main.splash", `
module main
use reports as local

fn summarize() needs Agent -> Int { return local.count() }
`)
	writeSplashFile(t, dir, "reports.splash", `
module reports
redline fn count() -> Int { return 42 }
`)
	program, err := analyzeProgram(entry)
	if err != nil {
		t.Fatal(err)
	}
	_, result, _ := compiler.Verify(program)
	diags := result.Violations
	if len(diags) != 1 || !strings.Contains(diags[0].Message, "redline fn") {
		t.Fatalf("expected the existing redline rule to apply to a qualified target, got %v", diags)
	}
}

func TestNestedApprovalCallRejectsUnsupportedLowering(t *testing.T) {
	entry := writeSplashFile(t, t.TempDir(), "main.splash", `
module main
approve fn standardPrice() -> Int { return 42 }
fn quotedPrice() -> Int { return standardPrice() }
fn main() { println(quotedPrice()) }
`)
	program, err := analyzeProgram(entry)
	if err != nil {
		t.Fatal(err)
	}
	_, result, _ := compiler.Verify(program)
	diags := result.Violations
	if len(diags) != 1 || !strings.Contains(diags[0].Message, "approval call") {
		t.Fatalf("expected one diagnostic for the unsupported nested approval call, got %v", diags)
	}
	if out, err := captureStdout(t, func() error { return runEmit(entry) }); err == nil || out != "" {
		t.Fatalf("emit = %q, %v; want no artifact", out, err)
	}
}

func TestFlatFunctionAmbiguitySurvivesInterveningNamespace(t *testing.T) {
	dir := t.TempDir()
	entry := writeSplashFile(t, dir, "main.splash", `
module main
use rates as first
use constants as price
use shipping as second

fn main() { println(price()) }
`)
	writeSplashFile(t, dir, "rates.splash", "module rates\nfn price() -> Int { return 40 }\n")
	writeSplashFile(t, dir, "constants.splash", "module constants\nfn tax() -> Int { return 1 }\n")
	writeSplashFile(t, dir, "shipping.splash", "module shipping\nfn price() -> Int { return 2 }\n")
	if err := runCheck(entry); err == nil {
		t.Fatal("check lost the ambiguous flat name after a namespace import")
	}
}

func TestDuplicateLocalFunctionSurvivesInterveningType(t *testing.T) {
	entry := writeSplashFile(t, t.TempDir(), "main.splash", `
module main
fn price() -> Int { return 40 }
type price { value: Int }
fn price() -> Int { return 2 }
fn main() { println(price()) }
`)
	if err := runCheck(entry); err == nil {
		t.Fatal("check accepted duplicate declarations separated by a type")
	}
}

func TestVM001HelloMatchesGoAndExpectedOutput(t *testing.T) {
	dir := t.TempDir()
	path := writeSplashFile(t, dir, "hello.splash", `module main
fn main() {
    println("Hello, world")
}
`)
	program, err := analyzeProgram(path)
	if err != nil {
		t.Fatal(err)
	}
	verified := requireVerified(t, program)
	writeSplashFile(t, dir, "hello.splash", `module main
fn main() {
    println("different")
}
`)
	var vmOutput bytes.Buffer
	if err := verified.RunVM(&vmOutput, vm.Limits{Instructions: 100, Frames: 1}); err != nil {
		t.Fatal(err)
	}
	const want = "Hello, world\n"
	if got := vmOutput.String(); got != want {
		t.Fatalf("VM printed %q, want %q", got, want)
	}

	goSource, err := verified.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(dir, "hello.go")
	if err := os.WriteFile(generated, []byte(goSource), 0644); err != nil {
		t.Fatal(err)
	}
	goOutput, err := exec.Command("go", "run", generated).CombinedOutput()
	if err != nil {
		t.Fatalf("run generated Go: %v\n%s", err, goOutput)
	}
	if got := string(goOutput); got != want {
		t.Fatalf("Go printed %q, want %q", got, want)
	}
}

func TestVM001RejectsUnsupportedSourceBeforeOutput(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{"float", `println(4.2)`},
		{"short circuit", `println(true && false)`},
		{"guard", `guard true else { println("yes") }`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeSplashFile(t, t.TempDir(), "unsupported.splash", "module main\nfn main() {\nprintln(\"first\")\n"+test.body+"\n}\n")
			program, err := analyzeProgram(path)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			err = requireVerified(t, program).RunVM(&out, vm.Limits{Instructions: 100, Frames: 1})
			var vmErr *compiler.VMAdmissionError
			if !errors.As(err, &vmErr) || vmErr.Source.Line != 4 {
				t.Fatalf("want unsupported source diagnostic at line 4, got %v", err)
			}
			if out.Len() != 0 {
				t.Fatalf("unsupported input produced output %q", out.String())
			}
		})
	}
}

func TestVMReportsUnsupportedHostImportLocation(t *testing.T) {
	dir := t.TempDir()
	path := writeSplashFile(t, dir, "main.splash", "module main\nuse std/ai\nfn main() {}\n")
	program, err := analyzeProgram(path)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = requireVerified(t, program).RunVM(&out, vm.Limits{Instructions: 100, Frames: 1})
	var vmErr *compiler.VMAdmissionError
	if !errors.As(err, &vmErr) ||
		vmErr.Source.File != path || vmErr.Source.Line != 2 {
		t.Fatalf("want unsupported import at %s:2, got %v", path, err)
	}
}

const vmFactorial = `module main
fn factorial(n: Int) -> Int {
    if n <= 1 {
        return 1
    }
    let previous = factorial(n - 1)
    return n * previous
}
fn main() {
    println(factorial(0))
    println(factorial(5))
}
`

func TestVM002RecursiveExecutionMatchesGoAndExpectedOutput(t *testing.T) {
	dir := t.TempDir()
	path := writeSplashFile(t, dir, "recursive.splash", vmFactorial)
	program, err := analyzeProgram(path)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := requireVerified(t, program).RunVM(&out, vm.Limits{Instructions: 1000, Frames: 8}); err != nil {
		t.Fatal(err)
	}
	const want = "1\n120\n"
	if out.String() != want {
		t.Fatalf("VM printed %q, want %q", out.String(), want)
	}
	goSource, err := requireVerified(t, program).EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(dir, "recursive.go")
	if err := os.WriteFile(generated, []byte(goSource), 0644); err != nil {
		t.Fatal(err)
	}
	actual, err := exec.Command("go", "run", generated).CombinedOutput()
	if err != nil || string(actual) != want {
		t.Fatalf("Go returned %q, %v, want %q", actual, err, want)
	}
}

func TestVM002CountsAcrossFrames(t *testing.T) {
	path := writeSplashFile(t, t.TempDir(), "recursive.splash", vmFactorial)
	program, err := analyzeProgram(path)
	if err != nil {
		t.Fatal(err)
	}
	verified := requireVerified(t, program)
	for _, tc := range []struct {
		limits vm.Limits
		code   vm.ErrorCode
	}{
		{vm.Limits{Instructions: 45, Frames: 8}, vm.InstructionLimit},
		{vm.Limits{Instructions: 1000, Frames: 2}, vm.FrameLimit},
	} {
		var out bytes.Buffer
		err := verified.RunVM(&out, tc.limits)
		var vmErr *vm.Error
		if !errors.As(err, &vmErr) || vmErr.Code != tc.code || vmErr.FrameDepth < 2 {
			t.Fatalf("want %s in a nested frame, got %#v", tc.code, err)
		}
		if tc.code == vm.InstructionLimit && vmErr.InstructionCount != tc.limits.Instructions {
			t.Fatalf("used %d dispatches, limit %d", vmErr.InstructionCount, tc.limits.Instructions)
		}
	}
}

func TestVM002IntegerLiteralsRespectSigned64BitRange(t *testing.T) {
	for _, source := range []string{"9223372036854775808", "-9223372036854775809"} {
		path := writeSplashFile(t, t.TempDir(), "range.splash", "module main\nfn main() { println("+source+") }\n")
		if _, err := analyzeProgram(path); err == nil {
			t.Fatalf("out-of-range literal %s passed analysis", source)
		}
	}
	path := writeSplashFile(t, t.TempDir(), "range.splash", "module main\nfn main() { println(-9223372036854775808) }\n")
	program, err := analyzeProgram(path)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := requireVerified(t, program).RunVM(&out, vm.Limits{Instructions: 100, Frames: 1}); err != nil || out.String() != "-9223372036854775808\n" {
		t.Fatalf("minimum Int printed %q, error %v", out.String(), err)
	}
}

func TestVM002ArithmeticPrecedence(t *testing.T) {
	path := writeSplashFile(t, t.TempDir(), "precedence.splash", `module main
fn main() {
    println(1 + 2 * 3)
    println(1 < 2 + 3)
    println(9223372036854775807 + 1 * 0)
    println(20 - 6 - 3)
}
`)
	program, err := analyzeProgram(path)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = requireVerified(t, program).RunVM(&out, vm.Limits{Instructions: 1000, Frames: 1})
	const want = "7\ntrue\n9223372036854775807\n11\n"
	if err != nil || out.String() != want {
		t.Fatalf("VM returned %q, %v, want %q", out.String(), err, want)
	}
}

func TestCheckUnaryNegationTypes(t *testing.T) {
	for _, tc := range []struct {
		kind, operand string
		valid         bool
	}{
		{kind: "Int", operand: "42", valid: true},
		{kind: "Float", operand: "1.5", valid: true},
		{kind: "String", operand: `"hello"`},
		{kind: "Bool", operand: "true"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			source := "module main\nfn main() { let value: " + tc.kind + " = -" + tc.operand + "\nprintln(value)\n}\n"
			path := writeSplashFile(t, t.TempDir(), "negation.splash", source)
			err := runCheck(path)
			if tc.valid && err != nil {
				t.Fatalf("negating %s should pass check: %v", tc.kind, err)
			}
			if !tc.valid && err == nil {
				t.Fatalf("negating %s should fail check", tc.kind)
			}
		})
	}
}

func TestVM002EvaluatesOperandsAndCallArgumentsLeftToRight(t *testing.T) {
	path := writeSplashFile(t, t.TempDir(), "order.splash", `module main
fn trace(n: Int) -> Int {
    println(n)
    return n
}
fn add(a: Int, b: Int) -> Int {
    return a + b
}
fn main() {
    println(trace(4) + trace(2))
    println(add(trace(3), trace(1)))
}
`)
	program, err := analyzeProgram(path)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = requireVerified(t, program).RunVM(&out, vm.Limits{Instructions: 1000, Frames: 3})
	const want = "4\n2\n6\n3\n1\n4\n"
	if err != nil || out.String() != want {
		t.Fatalf("VM returned %q, %v, want %q", out.String(), err, want)
	}
}

func TestVM002RejectsMistypedIRBeforeExecution(t *testing.T) {
	for _, body := range []string{"println(1 + true)", "println(identity())", "if 1 { println(2) }"} {
		t.Run(body, func(t *testing.T) {
			path := writeSplashFile(t, t.TempDir(), "invalid.splash", "module main\nfn identity(n: Int) -> Int { return n }\nfn main() {\nprintln(1)\n"+body+"\n}\n")
			program, err := analyzeProgram(path)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			err = requireVerified(t, program).RunVM(&out, vm.Limits{Instructions: 100, Frames: 3})
			var validation *ir.ValidationError
			if !errors.As(err, &validation) || validation.Source.File != path || out.Len() != 0 ||
				!strings.Contains(err.Error(), path+":") || !strings.Contains(err.Error(), "SPLASH_VM_INVALID_IR") {
				t.Fatalf("want source-backed IR rejection before output, got %v and %q", err, out.String())
			}
		})
	}
}

func requireVerified(t *testing.T, program *compiler.AnalyzedProgram) *compiler.VerifiedProgram {
	t.Helper()
	verified, result, err := compiler.Verify(program)
	if err != nil {
		t.Fatalf("verify: %v (%v)", err, result)
	}
	return verified
}

func findFunction(t *testing.T, program *compiler.AnalyzedProgram, name string) compiler.Function {
	t.Helper()
	for _, function := range program.Functions() {
		if function.Name == name {
			return function
		}
	}
	t.Fatalf("no function named %q", name)
	return compiler.Function{}
}
