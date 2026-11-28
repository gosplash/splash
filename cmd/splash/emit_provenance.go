package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"gosplash.dev/splash/internal/compiler"
)

func runEmitJSON(path string, output io.Writer) error {
	program, err := analyzeProgram(path)
	if err != nil {
		return err
	}
	verified, err := verifyProgram(program)
	if err != nil {
		return err
	}
	digest, err := executableDigest()
	if err != nil {
		return fmt.Errorf("measure compiler executable: %w", err)
	}
	artifact, verification, err := verified.EmitGoWithProvenance(digest)
	if err != nil {
		return err
	}
	report := struct {
		SchemaVersion int                             `json:"schemaVersion"`
		Verification  compiler.VerificationProvenance `json:"verification"`
		Artifact      compiler.GoSourceArtifact       `json:"artifact"`
	}{SchemaVersion: 1, Verification: verification, Artifact: artifact}
	if err := json.NewEncoder(output).Encode(report); err != nil {
		return fmt.Errorf("write emission report: %w", err)
	}
	return nil
}

func executableDigest() ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	path, err := os.Executable()
	if err != nil {
		return sum, err
	}
	executable, err := os.Open(path)
	if err != nil {
		return sum, err
	}
	defer executable.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, executable); err != nil {
		return sum, err
	}
	copy(sum[:], digest.Sum(nil))
	return sum, nil
}
