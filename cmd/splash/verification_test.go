package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/compiler"
	"gosplash.dev/splash/internal/safety"
)

func TestIncompletePolicyCoverageStopsArtifactCommands(t *testing.T) {
	dir := t.TempDir()
	const source = `module main
fn passthrough(n: Int) -> Int { return n }
tool fn quote() -> Int { return passthrough(%s) }
fn main() { println("output") }
`
	path := writeSplashFile(t, dir, "main.splash", fmt.Sprintf(source, "match 1 { 1 => 2 }"))
	artifact := filepath.Join(dir, "program")
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"check", func() error { return runCheck(path) }},
		{"emit", func() error { return runEmit(path) }},
		{"build", func() error { return runBuild(path, artifact) }},
		{"tools", func() error { return runTools(path, compiler.FormatOpenAI) }},
		{"run", func() error { return runVMCommand([]string{path, "--backend", "vm"}, os.Stdout) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, diag, err := captureEffectCommand(t, tc.run)
			if !errors.Is(err, safety.ErrIndeterminate) || output != "" ||
				!strings.Contains(diag, path+":3:") || !strings.Contains(diag, "unsupported expression") {
				t.Fatalf("want indeterminate verification before output, got %v, %q:\n%s", err, output, diag)
			}
		})
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("build left an artifact or unexpected stat failure: %v", err)
	}
	output, err := captureStdout(t, func() error { return runEffects(path) })
	if err != nil || !strings.Contains(output, "coverage: incomplete") {
		t.Fatalf("want partial inspection with coverage, got %v:\n%s", err, output)
	}

	writeSplashFile(t, dir, "main.splash", fmt.Sprintf(source, "2"))
	if output, err := captureStdout(t, func() error { return runEmit(path) }); err != nil ||
		!strings.Contains(output, "func quote()") {
		t.Fatalf("want Go output for covered source, got %v: %s", err, output)
	}
	if err := runBuild(path, artifact); err != nil {
		t.Fatalf("want a Go artifact for covered source: %v", err)
	}
	if output, err := captureStdout(t, func() error { return runTools(path, compiler.FormatOpenAI) }); err != nil ||
		!strings.Contains(output, `"quote"`) {
		t.Fatalf("want tool manifest for covered source, got %v: %s", err, output)
	}
}

func TestIncompleteCoverageDoesNotHideKnownPolicyViolation(t *testing.T) {
	path := writeSplashFile(t, t.TempDir(), "main.splash", `module main
redline fn history() -> Int { return 1 }
agent fn status(callback: fn() -> Int) -> Int {
    let external = callback()
    return external + history()
}
`)
	output, diag, err := captureEffectCommand(t, func() error { return runCheck(path) })
	if !errors.Is(err, safety.ErrRejected) || output != "" ||
		!strings.Contains(diag, `redline fn "history"`) || !strings.Contains(diag, "unresolved call") {
		t.Fatalf("want rejection reporting both the known violation and the gap, got %v, %q:\n%s", err, output, diag)
	}
}

func TestOpaqueExpressionCannotHideUnknownCallFromPolicyAnalysis(t *testing.T) {
	path := writeSplashFile(t, t.TempDir(), "main.splash", `module main
fn passthrough(n: Int) -> Int { return n }
fn helper(callback: fn() -> Int) -> Int {
    return passthrough(match 1 { 1 => callback() })
}
agent fn lookup() -> Int { return 1 }
fn reports(callback: fn() -> Int) needs DB.read -> Int { return helper(callback) }
`)
	output, diag, err := captureEffectCommand(t, func() error { return runCheck(path) })
	if !errors.Is(err, safety.ErrIndeterminate) || output != "" ||
		!strings.Contains(diag, path+":4:") || !strings.Contains(diag, "unsupported expression") {
		t.Fatalf("want indeterminate declared authority at nested match, got %v, %q:\n%s", err, output, diag)
	}
}

func TestApprovalBoundaryCannotHideUncheckedBody(t *testing.T) {
	for _, tc := range []struct{ name, source, line string }{
		{"other function", "approve fn authorize() -> Int { return 1 }\nfn summarize() -> Int { return match 1 { 1 => authorize() } }", "3"},
		{"approval body", "approve fn authorize() -> Int { return match 1 { 1 => 1 } }", "2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSplashFile(t, t.TempDir(), "main.splash", "module main\n"+tc.source+"\n")
			output, diag, err := captureEffectCommand(t, func() error { return runEmit(path) })
			if !errors.Is(err, safety.ErrIndeterminate) || output != "" ||
				!strings.Contains(diag, path+":"+tc.line+":") || !strings.Contains(diag, "unsupported expression") {
				t.Fatalf("want indeterminate approval propagation through opaque body, got %v, %q:\n%s", err, output, diag)
			}
		})
	}
}

func TestVerificationRequiresCoverageForToolAndSandboxRoots(t *testing.T) {
	const source = `module main
fn helper(callback: fn() -> Int) -> Int { return callback() }
%sfn report(callback: fn() -> Int) -> Int { return helper(callback) }
`
	for _, tc := range []struct{ name, annotation string }{
		{"conventional callback", ""},
		{"sandbox", "@sandbox(allow: [])\n"},
		{"tool", "tool "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSplashFile(t, t.TempDir(), "main.splash", fmt.Sprintf(source, tc.annotation))
			_, diag, err := captureEffectCommand(t, func() error { return runCheck(path) })
			if tc.annotation == "" {
				if err != nil {
					t.Fatalf("conventional callback should still pass, got %v: %s", err, diag)
				}
				return
			}
			if !errors.Is(err, safety.ErrIndeterminate) || !strings.Contains(diag, "unresolved call target") {
				t.Fatalf("want indeterminate %s root, got %v: %s", tc.name, err, diag)
			}
		})
	}
}

func TestApprovalHostGapDoesNotMaskTransitiveSourceGap(t *testing.T) {
	path := writeSplashFile(t, t.TempDir(), "main.splash", `module main
approve fn authorize() -> Int { return 1 }
fn calculate() -> Int { return match 1 { 1 => 2 } }
agent fn status() -> Int {
    let approval = authorize()
    let value = calculate()
    return approval + value
}
`)
	output, err := captureStdout(t, func() error { return runEffects(path) })
	_, status, found := strings.Cut(output, "- status:")
	if err != nil || !found || !strings.Contains(status, "unsupported expression") {
		t.Fatalf("want the source gap for status, got %v:\n%s", err, output)
	}
	output, diagnostic, err := captureEffectCommand(t, func() error { return runEmit(path) })
	if !errors.Is(err, safety.ErrIndeterminate) || output != "" ||
		!strings.Contains(diagnostic, path+":3:") || !strings.Contains(diagnostic, "unsupported expression") {
		t.Fatalf("want indeterminate emit at transitive source gap, got %v, %q:\n%s", err, output, diagnostic)
	}
}

func TestExplicitRecordFieldDoesNotUseDefault(t *testing.T) {
	path := writeSplashFile(t, t.TempDir(), "main.splash", `module main
fn fallback() -> Int { return 99 }
type Quote { total: Int = fallback() }
agent fn quote() -> Int {
    let result = Quote { total: 42 }
    return result.total
}
`)
	if _, diag, err := captureEffectCommand(t, func() error { return runCheck(path) }); err != nil {
		t.Fatalf("an explicitly supplied field should not require its default, got %v:\n%s", err, diag)
	}
}

func TestRequiredCoverageReportsAllUnresolvedCallSites(t *testing.T) {
	path := writeSplashFile(t, t.TempDir(), "main.splash", `module main
fn details(first: fn() -> Int, second: fn() -> Int) -> Int {
    let value = first()
    return value + second()
}
fn extra(callback: fn() -> Int) -> Int { return callback() }
agent fn report(first: fn() -> Int, second: fn() -> Int) -> Int {
    return details(first, second) + extra(first)
}
agent fn summary(first: fn() -> Int, second: fn() -> Int) -> Int {
    return details(first, second)
}
`)
	var previous string
	for range 5 {
		_, diag, err := captureEffectCommand(t, func() error { return runCheck(path) })
		if !errors.Is(err, safety.ErrIndeterminate) || !strings.Contains(diag, path+":3:") ||
			!strings.Contains(diag, path+":4:") || !strings.Contains(diag, path+":6:") ||
			strings.Count(diag, "unresolved call target") != 3 {
			t.Fatalf("want each uncovered call site once, got %v:\n%s", err, diag)
		}
		if previous != "" && diag != previous {
			t.Fatalf("coverage diagnostics changed:\n%s\n%s", previous, diag)
		}
		previous = diag
	}
}

func TestDeclaredEffectRejectionKeepsCoverageInspectable(t *testing.T) {
	path := writeSplashFile(t, t.TempDir(), "main.splash", `module main
fn read() needs DB.read -> Int { return 1 }
agent fn report(callback: fn() -> Int) -> Int {
    let value = callback()
    return value + read()
}
`)
	output, diag, err := captureEffectCommand(t, func() error { return runCheck(path) })
	if !errors.Is(err, safety.ErrRejected) || output != "" ||
		!strings.Contains(diag, "missing declared effect DB.read") || !strings.Contains(diag, "unresolved call target") {
		t.Fatalf("want rejection with contract violation and uncovered call, got %v, %q:\n%s", err, output, diag)
	}
	inspection, err := captureStdout(t, func() error { return runEffects(path) })
	if err != nil || !strings.Contains(inspection, "coverage: incomplete") {
		t.Fatalf("want effect inspection even for a rejected contract, got %v: %s", err, inspection)
	}
}

func TestGoApprovalHelperIgnoresOrdinaryParameterNames(t *testing.T) {
	dir := t.TempDir()
	path := writeSplashFile(t, dir, "main.splash", `module main
approve fn price(splashApprove: Int) -> Int { return splashApprove + 2 }
fn main() {
    let amount = price(40)
    println(amount)
}
`)
	binary := filepath.Join(dir, "approved_price")
	if err := runBuild(path, binary); err != nil {
		t.Fatalf("approval helper must have its own binding: %v", err)
	}
	command := exec.Command(binary)
	command.Stdin = strings.NewReader("y\n")
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "[approve] price") || !strings.HasSuffix(string(output), "42\n") {
		t.Fatalf("want approval prompt followed by price 42, got %v: %s", err, output)
	}
}

func TestLiteralToolDefaultHasCompleteSourceCoverage(t *testing.T) {
	path := writeSplashFile(t, t.TempDir(), "main.splash", `module main
tool fn report(limit: Int = 5) -> Int { return limit }
`)
	out, diag, err := captureEffectCommand(t, func() error { return runTools(path, compiler.FormatOpenAI) })
	if err != nil || !strings.Contains(out, `"limit"`) || strings.Contains(out, `"required"`) {
		t.Fatalf("want exportable tool with literal default, got %v: %q\n%s", err, out, diag)
	}
}

func TestSourceVerificationDoesNotClaimNativeBuildSuccess(t *testing.T) {
	dir := t.TempDir()
	path := writeSplashFile(t, dir, "library.splash", "module library\nfn count() -> Int { return 1 }\n")
	if err := runCheck(path); err != nil {
		t.Fatalf("library should verify as source: %v", err)
	}
	binary := filepath.Join(dir, "library.exe")
	_, diagnostic, err := captureEffectCommand(t, func() error { return runBuild(path, binary) })
	var nativeFailure *exec.ExitError
	if !errors.As(err, &nativeFailure) || !strings.Contains(diagnostic, "function main is undeclared") {
		t.Fatalf("want a native build failure for a library without main, got %v:\n%s", err, diagnostic)
	}
	if _, err := os.Stat(binary); !os.IsNotExist(err) {
		t.Fatalf("native build left an artifact or unexpected stat result: %v", err)
	}
}
