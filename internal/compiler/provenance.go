package compiler

import (
	"crypto/sha256"
	"fmt"
	"unicode/utf8"
)

const (
	verificationIdentityPrefix = "splash-verification-v1:sha256:"
	artifactIdentityPrefix     = "splash-artifact-v1:sha256:"
	sourcePolicyScope          = "required-source-policies"
	// Bump when the source verification rules or required scope change.
	verificationRulesVersion = "1"
)

// VerificationProvenance identifies the recorded inputs of source verification.
// A protected verifier must establish the compiler digest independently.
type VerificationProvenance struct {
	AnalysisIdentity
	VerificationID string `json:"verificationId"`
	CompilerDigest string `json:"compilerDigest"`
	RulesVersion   string `json:"rulesVersion"`
	Scope          string `json:"scope"`
	EffectCoverage string `json:"effectCoverage"`
}

type GoSourceArtifact struct {
	Kind       string `json:"kind"`
	Source     string `json:"source"`
	Digest     string `json:"digest"`
	ArtifactID string `json:"artifactId"`
}

// EmitGoWithProvenance binds generated Go source bytes to their verified
// capture. The caller supplies the measured executable digest.
func (v *VerifiedProgram) EmitGoWithProvenance(compilerDigest [sha256.Size]byte) (GoSourceArtifact, VerificationProvenance, error) {
	source, err := v.EmitGo()
	if err != nil {
		return GoSourceArtifact{}, VerificationProvenance{}, err
	}
	if !utf8.ValidString(source) {
		return GoSourceArtifact{}, VerificationProvenance{}, fmt.Errorf("emitted Go is not valid UTF-8")
	}
	evidence := VerificationProvenance{
		AnalysisIdentity: (&AnalyzedProgram{snapshot: v.verified}).Identity(),
		CompilerDigest:   formatSHA256(compilerDigest),
		RulesVersion:     verificationRulesVersion,
		Scope:            sourcePolicyScope,
		EffectCoverage:   "complete",
	}
	if !v.effectsComplete {
		evidence.EffectCoverage = "incomplete"
	}
	evidence.VerificationID = manifestIdentity(verificationIdentityPrefix,
		evidence.AnalysisID, evidence.CompilerDigest, evidence.RulesVersion, evidence.Scope, evidence.EffectCoverage)
	artifact := GoSourceArtifact{Kind: "go-source", Source: source, Digest: formatSHA256(sha256.Sum256([]byte(source)))}
	artifact.ArtifactID = manifestIdentity(artifactIdentityPrefix, evidence.VerificationID, artifact.Kind, artifact.Digest)
	return artifact, evidence, nil
}

func formatSHA256(sum [sha256.Size]byte) string {
	return fmt.Sprintf("sha256:%x", sum)
}

func manifestIdentity(prefix string, fields ...string) string {
	manifest := appendField(nil, prefix)
	for _, field := range fields {
		manifest = appendField(manifest, field)
	}
	return fmt.Sprintf("%s%x", prefix, sha256.Sum256(manifest))
}
