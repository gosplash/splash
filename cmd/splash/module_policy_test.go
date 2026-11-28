package main

import (
	"fmt"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/compiler"
)

func TestContainmentFollowsResolvedModuleOfNestedCall(t *testing.T) {
	dir := t.TempDir()
	writeSplashFile(t, dir, "inventory.splash", `module inventory
expose count
fn count() -> Int { return 1 }
`)
	restricted := writeSplashFile(t, dir, "archive.splash", `@containment(agent: "none")
module archive
expose count
fn count() -> Int { return 2 }
`)
	const entry = `module app
use inventory as active
use archive as old
agent fn status() -> Int { return 1 + %s.count() }
`
	path := writeSplashFile(t, dir, "app.splash", fmt.Sprintf(entry, "active"))
	if _, diagnostic, err := captureEffectCommand(t, func() error { return runCheck(path) }); err != nil {
		t.Fatalf("unrestricted declaration should pass, got %v: %s", err, diagnostic)
	}

	writeSplashFile(t, dir, "app.splash", fmt.Sprintf(entry, "old"))
	_, diagnostic, err := captureEffectCommand(t, func() error { return runCheck(path) })
	if err == nil || !strings.Contains(diagnostic, restricted+":4:") ||
		!strings.Contains(diagnostic, `module has @containment(agent: "none")`) {
		t.Fatalf("want containment failure attributed to imported archive.count, got %v: %s", err, diagnostic)
	}
}

func TestToolClassificationUsesResolvedRecordDeclaration(t *testing.T) {
	dir := t.TempDir()
	writeSplashFile(t, dir, "public_data.splash", `module public_data
expose Payload
type Payload { value: String }
`)
	writeSplashFile(t, dir, "private_data.splash", `module private_data
expose Payload
type Payload {
    @restricted
    secret: String
}
`)
	const entry = `module app
use public_data as visible
use private_data as hidden
tool fn lookup(value: %s.Payload) -> %s.Payload? { return value }
`
	path := writeSplashFile(t, dir, "app.splash", fmt.Sprintf(entry, "visible", "visible"))
	output, diagnostic, err := captureEffectCommand(t, func() error { return runTools(path, compiler.FormatOpenAI) })
	if err != nil || !strings.Contains(output, `"lookup"`) {
		t.Fatalf("public record should allow the tool export, got %v: %s", err, diagnostic)
	}

	writeSplashFile(t, dir, "app.splash", fmt.Sprintf(entry, "hidden", "hidden"))
	output, diagnostic, err = captureEffectCommand(t, func() error { return runTools(path, compiler.FormatOpenAI) })
	if err == nil || output != "" || !strings.Contains(diagnostic, "sensitive or restricted fields (secret)") {
		t.Fatalf("want classified record rejection and no tool manifest, got %v, %q: %s", err, output, diagnostic)
	}
}

func TestToolClassificationAcrossResolvedReturnShapes(t *testing.T) {
	binary := buildSplashCLI(t)
	for _, tc := range []struct {
		name, typ, verdict, code string
	}{
		{"public list", "List<visible.Envelope>?", "verified", ""},
		{"classified list", "List<hidden.Envelope>?", "rejected", "POLICY_VIOLATION"},
		{"classified result success", "Result<hidden.Label, String>", "rejected", "POLICY_VIOLATION"},
		{"classified result error", "Result<String, hidden.Label>", "rejected", "POLICY_VIOLATION"},
		{"unknown element", "List<Missing>", "indeterminate", "POLICY_COVERAGE_INCOMPLETE"},
		{"recursive record", "visible.Cycle", "verified", ""},
		{"enum without payloads", "List<visible.Shape>", "verified", ""},
		{"enum with payloads", "visible.LabelKind", "indeterminate", "POLICY_COVERAGE_INCOMPLETE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeSplashFile(t, dir, "visible.splash", `module visible
type Envelope { labels: List<Label> }
type Label { text: String }
type Cycle { next: Cycle? }
enum Shape { circle square }
enum LabelKind { empty text(Label) }
`)
			writeSplashFile(t, dir, "hidden.splash", `module hidden
type Envelope { labels: List<Label> }
type Label { @sensitive text: String }
`)
			path := writeSplashFile(t, dir, "main.splash", fmt.Sprintf(`module main
use visible
use hidden
tool fn copy(labels: %[1]s) -> %[1]s { return labels }
`, tc.typ))
			diagnostics, _ := readCheckJSON(t, binary, path, tc.verdict)
			if tc.code == "" && len(diagnostics) != 0 ||
				tc.code != "" && (len(diagnostics) != 1 || diagnostics[0].Code != tc.code) {
				t.Fatalf("want %q for %s, got %+v", tc.code, tc.typ, diagnostics)
			}
		})
	}
}

func TestToolClassificationReportsKnownViolationsWithMissingEvidence(t *testing.T) {
	binary := buildSplashCLI(t)
	path := writeSplashFile(t, t.TempDir(), "main.splash", `module main
type Other { @restricted number: Int }
tool fn copy<T>(value: Result<T, Other>) -> Result<T, Other> { return value }
`)
	diagnostics, _ := readCheckJSON(t, binary, path, "rejected")
	var violation, gap bool
	for _, diagnostic := range diagnostics {
		violation = violation || diagnostic.Code == "POLICY_VIOLATION"
		gap = gap || diagnostic.Code == "POLICY_COVERAGE_INCOMPLETE"
	}
	if !violation || !gap {
		t.Fatalf("want both a known classification violation and a coverage gap, got %+v", diagnostics)
	}
}

func TestApprovedOnlyChecksAgentCallsIntoOtherAgentRoots(t *testing.T) {
	binary := buildSplashCLI(t)
	dir := t.TempDir()
	const module = `@containment(agent: "approved_only")
module catalog
expose count
%s fn count() needs Agent -> Int { return 3 }
`
	for _, tc := range []struct{ name, annotation, caller, verdict string }{
		{"standalone root", "", "fn main() {}", "verified"},
		{"inbound call", "", "agent fn report() -> Int { return count() }", "rejected"},
		{"explicit permission", "@agent_allowed", "agent fn report() -> Int { return count() }", "verified"},
		{"approval gate", "approve", "agent fn report() -> Int { return count() }", "verified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := writeSplashFile(t, dir, "catalog.splash", fmt.Sprintf(module, tc.annotation))
			path := writeSplashFile(t, dir, "main.splash", "module main\nuse catalog\n"+tc.caller+
				"\nfn count() -> Int { return catalog.count() }\n")
			diagnostics, _ := readCheckJSON(t, binary, path, tc.verdict)
			if tc.verdict == "rejected" && (len(diagnostics) != 1 || diagnostics[0].Code != "POLICY_VIOLATION" ||
				diagnostics[0].Source == nil || diagnostics[0].Source.Start.File != catalog) {
				t.Fatalf("want inbound containment diagnostic for catalog.count, got %+v", diagnostics)
			}
		})
	}
}
