package compiler_test

import (
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/compiler"
)

func TestGoSourceProvenanceRetainsVerifiedCapture(t *testing.T) {
	dir := t.TempDir()
	path := writeSource(t, dir, "main.splash", `module app
fn apply(cb: fn() -> Int) -> Int { return cb() }
fn main() { println("original") }
`)
	program := analyze(t, path)
	verified := verify(t, program)
	firstBuild := sha256.Sum256([]byte("first compiler build"))
	secondBuild := sha256.Sum256([]byte("second compiler build"))

	artifact, evidence, err := verified.EmitGoWithProvenance(firstBuild)
	if err != nil || evidence.AnalysisIdentity != program.Identity() || evidence.EffectCoverage != "incomplete" ||
		!strings.Contains(artifact.Source, "original") {
		t.Fatalf("unexpected provenance: %+v, %+v (%v)", artifact, evidence, err)
	}

	writeSource(t, dir, "main.splash", `module app
fn apply(cb: fn() -> Int) -> Int { return cb() }
fn main() { println("original") }
// This changes the capture without changing generated Go.
`)
	sameArtifact, sameEvidence, err := verified.EmitGoWithProvenance(firstBuild)
	if err != nil || sameArtifact != artifact || sameEvidence != evidence {
		t.Fatalf("verified artifact changed after source edit: %+v, %+v (%v)", sameArtifact, sameEvidence, err)
	}

	nextArtifact, nextEvidence, err := verify(t, analyze(t, path)).EmitGoWithProvenance(firstBuild)
	if err != nil || nextArtifact.Digest != artifact.Digest || nextArtifact.ArtifactID == artifact.ArtifactID ||
		nextEvidence.VerificationID == evidence.VerificationID {
		t.Fatalf("changed capture was not linked to equal artifact bytes: %+v, %+v (%v)", nextArtifact, nextEvidence, err)
	}

	otherArtifact, otherEvidence, err := verified.EmitGoWithProvenance(secondBuild)
	if err != nil || otherArtifact.Digest != artifact.Digest || otherEvidence.AnalysisIdentity != evidence.AnalysisIdentity ||
		otherEvidence.CompilerDigest == evidence.CompilerDigest || otherEvidence.VerificationID == evidence.VerificationID ||
		otherArtifact.ArtifactID == artifact.ArtifactID {
		t.Fatalf("compiler build mismatch did not affect provenance: %+v, %+v (%v)", otherArtifact, otherEvidence, err)
	}
}

func TestGoSourceProvenanceRequiresVerifiedInput(t *testing.T) {
	for _, handle := range []*compiler.VerifiedProgram{nil, {}} {
		artifact, evidence, err := handle.EmitGoWithProvenance(sha256.Sum256([]byte("compiler")))
		if !errors.Is(err, compiler.ErrUnverifiedProgram) || artifact != (compiler.GoSourceArtifact{}) ||
			evidence != (compiler.VerificationProvenance{}) {
			t.Fatalf("unverified input produced evidence: %+v, %+v (%v)", artifact, evidence, err)
		}
	}
}
