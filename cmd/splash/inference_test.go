package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/compiler"
	"gosplash.dev/splash/internal/safety"
)

func TestEffectsInferThroughRecursiveCalls(t *testing.T) {
	path := writeSplashFile(t, t.TempDir(), "recursive.splash", `module recursive
use std/ai
fn ask(n: Int) needs AI -> Int {
    if n <= 0 { return ai.prompt<Int>("question") }
    return repeat(n - 1)
}
fn repeat(n: Int) needs AI -> Int { return ask(n) }
fn uncertain(cb: fn() -> Int) needs AI, DB.read -> Int { return repeat(1) + cb() }
fn caller(cb: fn() -> Int) needs AI, DB.read -> Int { return uncertain(cb) }
fn unused() needs DB.read -> Int { return 1 }
fn contract_only() needs DB.read -> Int { return unused() }
fn unknown(cb: fn() -> Int) needs DB.read -> Int { return cb() }
`)
	out, err := captureStdout(t, func() error { return runEffects(path) })
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		function string
		fields   []string
		absent   string
	}{
		{"ask", []string{"inferred: AI", "coverage: complete"}, "unused grants:"},
		{"repeat", []string{"inferred: AI", "coverage: complete"}, "unused grants:"},
		{"uncertain", []string{"inferred: AI (known)", "coverage: incomplete", "unresolved call"}, "unused grants:"},
		{"caller", []string{"inferred: AI (known)", "coverage: incomplete", "unresolved call"}, "unused grants:"},
		{"unused", []string{"inferred: none", "coverage: complete", "unused grants: DB.read"}, ""},
		{"contract_only", []string{"inferred: none", "coverage: complete"}, "unused grants:"},
		{"unknown", []string{"inferred: unknown", "coverage: incomplete"}, "unused grants:"},
	} {
		_, block, found := strings.Cut(out, "- "+tc.function+":")
		block, _, _ = strings.Cut(block, "\n- ")
		if !found || (tc.absent != "" && strings.Contains(block, tc.absent)) {
			t.Fatalf("unexpected summary for %q in:\n%s", tc.function, out)
		}
		for _, field := range tc.fields {
			if !strings.Contains(block, field) {
				t.Fatalf("summary for %q missing %q:\n%s", tc.function, field, block)
			}
		}
	}
}

func TestOptionalStdAICallRetainsItsEffectContract(t *testing.T) {
	dir := t.TempDir()
	const source = "module sample\nuse std/ai\nagent fn ask() %s -> Int { return ai?.prompt<Int>(\"query\") }\n"
	path := writeSplashFile(t, dir, "sample.splash", fmt.Sprintf(source, "needs AI"))
	if _, diagnostic, err := captureEffectCommand(t, func() error { return runCheck(path) }); err != nil {
		t.Fatalf("want declared AI checked as before, got %v: %s", err, diagnostic)
	}
	writeSplashFile(t, dir, "sample.splash", fmt.Sprintf(source, ""))
	if _, diagnostic, err := captureEffectCommand(t, func() error { return runCheck(path) }); err == nil ||
		!strings.Contains(diagnostic, "inferred effect AI") {
		t.Fatalf("want inferred AI violation, got %v: %s", err, diagnostic)
	}
}

func TestInferredEffectRejectionHasStableWitness(t *testing.T) {
	dir := t.TempDir()
	path := writeSplashFile(t, dir, "recursive.splash", `module recursive
use std/ai
fn ask(n: Int) -> Int {
    if n <= 0 { return ai.prompt<Int>("question") }
    return repeat(n - 1)
}
fn repeat(n: Int) -> Int { return ask(n) }
fn start(cb: fn() -> Int) -> Int { return repeat(1) + cb() }
`)
	report, err := captureStdout(t, func() error { return runEffects(path) })
	if err != nil || !strings.Contains(report, "- ask: none (declared)\n  inferred: AI") {
		t.Fatalf("want inspection of rejected implementation, got %v:\n%s", err, report)
	}
	const origin = `inferred effect AI exceeds "ask"'s declared authority: ask -> std/ai.prompt`
	var previous string
	for range 3 {
		output, diagnostic, err := captureEffectCommand(t, func() error { return runCheck(path) })
		if err == nil || output != "" || !strings.Contains(diagnostic, origin) ||
			!strings.Contains(diagnostic, "start -> repeat -> ask -> std/ai.prompt") ||
			!strings.Contains(diagnostic, "recursive.splash:4:") {
			t.Fatalf("want inferred-effect rejection with witness, got %v, %q, %q", err, output, diagnostic)
		}
		if previous != "" && diagnostic != previous {
			t.Fatalf("unstable diagnostics:\n%s\n%s", previous, diagnostic)
		}
		previous = diagnostic
	}
	artifact := filepath.Join(dir, "program.exe")
	for name, command := range map[string]func() error{
		"emit":  func() error { return runEmit(path) },
		"build": func() error { return runBuild(path, artifact) },
		"run":   func() error { return runVMCommand([]string{path, "--backend", "vm"}, os.Stdout) },
		"tools": func() error { return runTools(path, compiler.FormatOpenAI) },
	} {
		t.Run(name, func(t *testing.T) {
			output, diagnostic, err := captureEffectCommand(t, command)
			if err == nil || output != "" || !strings.Contains(diagnostic, origin) {
				t.Fatalf("want effect rejection before output, got %v, %q, %q", err, output, diagnostic)
			}
		})
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("want no artifact after rejection, got %v", err)
	}
}

func TestEffectsDoNotTrustShadowedAdapter(t *testing.T) {
	path := writeSplashFile(t, t.TempDir(), "shadow.splash", `module shadow
use std/ai
type AIAdapter { prompt: fn(String) -> Int }
fn shadowed(ai: AIAdapter) -> Int { return ai.prompt<Int>("question") }
`)
	out, err := captureStdout(t, func() error { return runEffects(path) })
	if err != nil || !strings.Contains(out, "inferred: unknown") || strings.Contains(out, "inferred: AI") {
		t.Fatalf("want incomplete shadowed adapter summary, got %v:\n%s", err, out)
	}
}

func TestEffectsFollowOwningModuleDeclaration(t *testing.T) {
	dir := t.TempDir()
	writeSplashFile(t, dir, "pure.splash", "module pure\nfn lookup() -> Int { return 1 }\n")
	writeSplashFile(t, dir, "provider.splash", `module provider
use std/ai
fn lookup() needs AI -> Int { return ai.prompt<Int>("question") }
`)
	path := writeSplashFile(t, dir, "entry.splash", `module entry
use pure as local
use provider as remote
fn a() -> Int { return local.lookup() }
fn b() needs AI -> Int { return remote.lookup() }
`)
	out, err := captureStdout(t, func() error { return runEffects(path) })
	if err != nil || !strings.Contains(out, "- a: none (declared)\n  inferred: none\n  coverage: complete") ||
		!strings.Contains(out, "- b: AI (declared)\n  inferred: AI\n  coverage: complete") {
		t.Fatalf("want independent summaries for same-named imported declarations, got %v:\n%s", err, out)
	}
}

func TestEffectsMarkUnmodeledApprovalIncomplete(t *testing.T) {
	path := writeSplashFile(t, t.TempDir(), "approval.splash", `module approval
approve fn gate() needs Net -> Int { return 1 }
fn caller() needs Net -> Int { return gate() }
`)
	out, err := captureStdout(t, func() error { return runEffects(path) })
	if err != nil || strings.Count(out, "approval adapter has no modeled effect contract") != 2 || strings.Contains(out, "unused grants:") {
		t.Fatalf("want incomplete coverage at gate and caller, got %v:\n%s", err, out)
	}
}

func TestEffectsPropagateUnsupportedSourceCoverage(t *testing.T) {
	for _, tc := range []struct{ name, declarations, expression, reason string }{
		{"parameter default", "fn leaf(n: Int = initial()) needs DB.read -> Int { return n }\nfn initial() -> Int { return 1 }", "leaf()", "parameter default was not analyzed"},
		{"record default", "type Record { value: Int = initial() }\nfn leaf() needs DB.read -> Int { let r = Record {}\nreturn r.value }\nfn initial() -> Int { return 1 }", "leaf()", "record field default was not analyzed"},
		{"match expression", "fn leaf() needs DB.read -> Int { return match 1 { 1 => 1 } }", "leaf()", "unsupported expression"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "module sample\n" + tc.declarations + "\nfn caller() needs DB.read -> Int { return " + tc.expression + " }\n"
			path := writeSplashFile(t, t.TempDir(), "sample.splash", source)
			out, err := captureStdout(t, func() error { return runEffects(path) })
			if err != nil || strings.Count(out, tc.reason+" at "+path+":2:") != 2 || strings.Contains(out, "unused grants:") {
				t.Fatalf("want propagated coverage gap at the source, got %v:\n%s", err, out)
			}
			_, diag, err := captureEffectCommand(t, func() error { return runCheck(path) })
			if !errors.Is(err, safety.ErrIndeterminate) || !strings.Contains(diag, path+":2:") ||
				!strings.Contains(diag, tc.reason) {
				t.Fatalf("want indeterminate source verification, got %v:\n%s", err, diag)
			}
		})
	}
}
