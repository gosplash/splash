package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestEmitJSONBindsVerifiedCaptureAndExactArtifactBytes(t *testing.T) {
	executablePath := buildSplashCLI(t)
	dir := t.TempDir()
	path := writeSplashFile(t, dir, "main.splash", `module app
fn main() { println("captured") }
`)
	read := func() (map[string]string, string) {
		t.Helper()
		raw, err := exec.Command(executablePath, "emit", path, "--json").Output()
		if err != nil {
			t.Fatal(err)
		}
		var report struct {
			SchemaVersion int
			Artifact      struct {
				Kind, Source, Digest, ArtifactID string
			}
			Verification struct {
				SourceID, AnalysisID, VerificationID, CompilerDigest, RulesVersion, Scope, EffectCoverage string
			}
		}
		if err := json.Unmarshal(raw, &report); err != nil || report.SchemaVersion != 1 {
			t.Fatalf("invalid emission report: %s (%v)", raw, err)
		}
		assertProvenanceKeys(t, raw)
		executable, err := os.ReadFile(executablePath)
		if err != nil {
			t.Fatal(err)
		}
		plainSource, err := exec.Command(executablePath, "emit", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		artifact, verification := report.Artifact, report.Verification
		if artifact.Kind != "go-source" || artifact.Source != string(plainSource) ||
			artifact.Digest != fmt.Sprintf("sha256:%x", sha256.Sum256(plainSource)) ||
			verification.CompilerDigest != fmt.Sprintf("sha256:%x", sha256.Sum256(executable)) ||
			verification.Scope != "required-source-policies" || verification.RulesVersion != "1" ||
			verification.EffectCoverage != "complete" ||
			verification.SourceID == "" || verification.AnalysisID == "" ||
			verification.VerificationID == "" || artifact.ArtifactID == "" {
			t.Fatalf("wrong artifact provenance: %s", raw)
		}
		matchManifest := func(prefix, got string, fields ...string) {
			t.Helper()
			hash := sha256.New()
			var length [binary.MaxVarintLen64]byte
			for _, field := range append([]string{prefix}, fields...) {
				n := binary.PutUvarint(length[:], uint64(len(field)))
				hash.Write(length[:n])
				io.WriteString(hash, field)
			}
			if expected := fmt.Sprintf("%s%x", prefix, hash.Sum(nil)); got != expected {
				t.Fatalf("identity %q, want %q", got, expected)
			}
		}
		matchManifest("splash-verification-v1:sha256:", verification.VerificationID,
			verification.AnalysisID, verification.CompilerDigest, verification.RulesVersion, verification.Scope, verification.EffectCoverage)
		matchManifest("splash-artifact-v1:sha256:", artifact.ArtifactID,
			verification.VerificationID, artifact.Kind, artifact.Digest)
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
		{"module app\nredline fn denied() {}\nagent fn report() { denied() }", "redline"},
	} {
		path := writeSplashFile(t, dir, "main.splash", tc.source)
		stdout, err := exec.Command(binary, "emit", path, "--json").Output()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 1 || len(stdout) != 0 || !strings.Contains(string(exit.Stderr), tc.diagnostic) {
			t.Fatalf("want diagnostic %q and no artifact, got %q (%v)", tc.diagnostic, stdout, err)
		}
	}
}

func TestEmitJSONArguments(t *testing.T) {
	binary := buildSplashCLI(t)
	path := writeSplashFile(t, t.TempDir(), "main.splash", "module app\nfn main() {}")
	trailing, err := exec.Command(binary, "emit", path, "--json").Output()
	if err != nil {
		t.Fatal(err)
	}
	leading, err := exec.Command(binary, "emit", "--json", path).Output()
	if err != nil || string(leading) != string(trailing) {
		t.Errorf("leading --json must select the same artifact report: %q (%v)", leading, err)
	}
	for _, flags := range [][]string{{"--json", "--json"}, {"--json", "extra"}, {"--bogus", "--json"}} {
		args := append([]string{"emit", path}, flags...)
		output, err := exec.Command(binary, args...).Output()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 1 || len(output) != 0 || !strings.Contains(string(exit.Stderr), "unexpected emit") {
			t.Errorf("malformed emit request %v produced %q (%v)", flags, output, err)
		}
	}
}

func assertProvenanceKeys(t *testing.T, raw []byte) {
	t.Helper()
	keys := func(raw []byte, want ...string) map[string]json.RawMessage {
		t.Helper()
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatal(err)
		}
		if got := slices.Sorted(maps.Keys(object)); !slices.Equal(got, want) {
			t.Fatalf("unexpected v1 keys: %v, want %v", got, want)
		}
		return object
	}
	report := keys(raw, "artifact", "schemaVersion", "verification")
	keys(report["artifact"], "artifactId", "digest", "kind", "source")
	keys(report["verification"], "analysisId", "compilerDigest", "effectCoverage", "rulesVersion", "scope", "sourceId", "verificationId")
}
