package compiler

import (
	"errors"

	"gosplash.dev/splash/internal/safety"
)

var (
	ErrInvalidAnalysis   = errors.New("verification requires an analyzed program")
	ErrUnverifiedProgram = errors.New("artifact production requires a verified program")
)

// VerifiedProgram authorizes the applicable source policies on one captured
// analysis. Backend admission, validation, and native compilation are separate.
type VerifiedProgram struct {
	// Keep a different struct shape from AnalyzedProgram. Go allows explicit
	// conversions between named structs with identical underlying types.
	verified *analysis
}

// Verify issues a handle only after all required source policy checks pass.
// Keep the immutable state itself so replacement of the caller's analysis
// handle cannot change what the issued handle authorizes.
func Verify(program *AnalyzedProgram) (*VerifiedProgram, safety.Result, error) {
	if program == nil || program.snapshot == nil {
		return nil, safety.Result{}, ErrInvalidAnalysis
	}
	snapshot := program.snapshot
	modules := make([]safety.Module, 0, len(snapshot.modules))
	for _, module := range snapshot.modules {
		modules = append(modules, safety.Module{File: module.File, Types: module.Types})
	}
	result := safety.New().Check(modules, snapshot.graph, snapshot.effects)
	switch {
	case len(result.Violations) > 0:
		return nil, result, safety.ErrRejected
	case len(result.Incomplete) > 0:
		return nil, result, safety.ErrIndeterminate
	default:
		return &VerifiedProgram{verified: snapshot}, result, nil
	}
}
