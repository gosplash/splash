package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestEmitJSONBindsVerifiedCaptureAndExactArtifactBytes(t *testing.T) {
	binary := buildSplashCLI(t)
	dir := t.TempDir()
	path := writeSplashFile(t, dir, "main.splash", `module app
fn main() { println("captured") }
`)
	read := func() (map[string]string, string) {
		t.Helper()
		raw, err := exec.Command(binary, "emit", path, "--json").Output()
		if err != nil {
			t.Fatal(err)
		}
		var report struct {
			SchemaVersion int
			Artifact      struct {
				Kind, Source, Digest, ArtifactID string
			}
			Verification struct {
				SourceID, AnalysisID, VerificationID, CompilerDigest, RulesVersion, Scope string
			}
		}
		if err := json.Unmarshal(raw, &report); err != nil || report.SchemaVersion != 1 {
			t.Fatalf("invalid emission report: %s (%v)", raw, err)
		}
		executable, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		plainSource, err := exec.Command(binary, "emit", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		artifact, verification := report.Artifact, report.Verification
		if artifact.Kind != "go-source" || artifact.Source != string(plainSource) ||
			artifact.Digest != fmt.Sprintf("sha256:%x", sha256.Sum256(plainSource)) ||
			verification.CompilerDigest != fmt.Sprintf("sha256:%x", sha256.Sum256(executable)) ||
			verification.Scope != "required-source-policies" || verification.RulesVersion == "" ||
			verification.SourceID == "" || verification.AnalysisID == "" ||
			verification.VerificationID == "" || artifact.ArtifactID == "" {
			t.Fatalf("wrong artifact provenance: %s", raw)
		}
		return map[string]string{
			"source": verification.SourceID, "analysis": verification.AnalysisID,
			"verification": verification.VerificationID, "artifact": artifact.ArtifactID,
		}, string(raw)
	}
	before, raw := read()
	if _, again := read(); again != raw {
		t.Fatal("JSON evidence differs for an identical compiler and capture")
	}
	writeSplashFile(t, dir, "main.splash", "module app\nfn main() { println(\"edited\") }\n")
	after, _ := read()
	for kind, id := range before {
		if after[kind] == id {
			t.Errorf("%s identity did not change with the captured source", kind)
		}
	}
}

func TestEmitJSONRejectsBeforeReportingArtifact(t *testing.T) {
	binary := buildSplashCLI(t)
	dir := t.TempDir()
	for _, tc := range []struct{ source, diagnostic string }{
		{"@containment(agent: \"invalid\")\nmodule app\nfn main() {}", "unknown @containment agent policy"},
		{"module app\nagent fn report(cb: fn() -> Int) -> Int { return cb() }", "unresolved"},
	} {
		path := writeSplashFile(t, dir, "main.splash", tc.source)
		stdout, err := exec.Command(binary, "emit", path, "--json").Output()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 1 || len(stdout) != 0 || !strings.Contains(string(exit.Stderr), tc.diagnostic) {
			t.Fatalf("want diagnostic %q and no artifact, got %q (%v)", tc.diagnostic, stdout, err)
		}
	}
}
