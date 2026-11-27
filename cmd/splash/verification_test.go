package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/safety"
	"gosplash.dev/splash/internal/toolschema"
)

func TestIncompletePolicyCoverageStopsArtifactCommands(t *testing.T) {
	dir := t.TempDir()
	path := writeSplashFile(t, dir, "main.splash", `module main
agent fn read(callback: fn() -> Int) -> Int { return 1 + callback() }
fn main() { println("output") }
`)
	artifact := filepath.Join(dir, "program")
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"check", func() error { return runCheck(path) }},
		{"emit", func() error { return runEmit(path) }},
		{"build", func() error { return runBuild(path, artifact) }},
		{"tools", func() error { return runTools(path, toolschema.FormatOpenAI) }},
		{"run", func() error { return runVMCommand([]string{path, "--backend", "vm"}, os.Stdout) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, diag, err := captureEffectCommand(t, tc.run)
			if !errors.Is(err, safety.ErrIndeterminate) || output != "" ||
				!strings.Contains(diag, path+":2:") || !strings.Contains(diag, "unresolved call") {
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
	path := writeSplashFile(t, t.TempDir(), "main.splash", `module main
approve fn authorize() -> Int { return 1 }
fn summarize() -> Int { return match 1 { 1 => authorize() } }
`)
	output, diag, err := captureEffectCommand(t, func() error { return runEmit(path) })
	if !errors.Is(err, safety.ErrIndeterminate) || output != "" ||
		!strings.Contains(diag, path+":3:") || !strings.Contains(diag, "unsupported expression") {
		t.Fatalf("want fail-closed approval propagation through opaque body, got %v, %q:\n%s", err, output, diag)
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
}
