package main

import (
	"fmt"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/toolschema"
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
	output, diagnostic, err := captureEffectCommand(t, func() error { return runTools(path, toolschema.FormatOpenAI) })
	if err != nil || !strings.Contains(output, `"lookup"`) {
		t.Fatalf("public record should allow the tool export, got %v: %s", err, diagnostic)
	}

	writeSplashFile(t, dir, "app.splash", fmt.Sprintf(entry, "hidden", "hidden"))
	output, diagnostic, err = captureEffectCommand(t, func() error { return runTools(path, toolschema.FormatOpenAI) })
	if err == nil || output != "" || !strings.Contains(diagnostic, "sensitive or restricted fields (secret)") {
		t.Fatalf("want classified record rejection and no tool manifest, got %v, %q: %s", err, output, diagnostic)
	}
}
