package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"gosplash.dev/splash/internal/compiler"
	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/safety"
	"gosplash.dev/splash/internal/token"
)

type checkReport struct {
	SchemaVersion  int                        `json:"schemaVersion"`
	EntryPath      string                     `json:"entryPath"`
	Verdict        *string                    `json:"verdict"`
	Identity       *compiler.AnalysisIdentity `json:"identity,omitempty"`
	EffectCoverage *checkEffectCoverage       `json:"effectCoverage"`
	Diagnostics    []checkDiagnostic          `json:"diagnostics"`
}

type checkEffectCoverage struct {
	Status string `json:"status"`
}

type checkDiagnostic struct {
	Code             string         `json:"code"`
	PolicyRuleID     string         `json:"policyRuleId,omitempty"`
	WitnessLength    int            `json:"witnessLength,omitempty"`
	WitnessTruncated *bool          `json:"witnessTruncated,omitempty"`
	Phase            string         `json:"phase"`
	Severity         string         `json:"severity"`
	Message          string         `json:"message"`
	ExpectedType     string         `json:"expectedType,omitempty"`
	ActualType       string         `json:"actualType,omitempty"`
	Source           *checkRange    `json:"source,omitempty"`
	Effects          []string       `json:"effects,omitempty"`
	Witness          []checkWitness `json:"witness,omitempty"`
}

type checkPoint struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type checkRange struct {
	Start checkPoint  `json:"start"`
	End   *checkPoint `json:"end,omitempty"`
}

type checkWitness struct {
	Kind        string      `json:"kind"`
	Target      string      `json:"target"`
	Source      *checkRange `json:"source,omitempty"`
	Declaration *checkPoint `json:"declaration,omitempty"`
}

func parseJSONFileArgs(command string, args []string) (path string, jsonMode bool, err error) {
	// Single file operands were accepted verbatim before either command had flags.
	if len(args) == 1 && args[0] != "" && args[0] != "--json" && args[0] != "--" {
		return args[0], false, nil
	}

	positionalOnly := false
	for _, arg := range args {
		switch {
		case !positionalOnly && arg == "--":
			positionalOnly = true
		case !positionalOnly && arg == "--json" && !jsonMode:
			jsonMode = true
		case !positionalOnly && strings.HasPrefix(arg, "-"):
			return "", false, fmt.Errorf("unexpected %s option %q", command, arg)
		case arg == "":
			return "", false, fmt.Errorf("%s requires a nonempty file path", command)
		case path == "":
			path = arg
		default:
			return "", false, fmt.Errorf("unexpected %s argument %q", command, arg)
		}
	}
	if path == "" {
		return "", false, fmt.Errorf("usage: splash %s <file.splash> [--json]", command)
	}
	return path, jsonMode, nil
}

func runCheckJSON(path string, output io.Writer) (bool, error) {
	verified, report := makeCheckReport(path)
	if err := json.NewEncoder(output).Encode(report); err != nil {
		return false, fmt.Errorf("write check report: %w", err)
	}
	return verified != nil, nil
}

func makeCheckReport(path string) (*compiler.VerifiedProgram, checkReport) {
	report := checkReport{SchemaVersion: 1, EntryPath: path, Diagnostics: []checkDiagnostic{}}
	program, diagnostics, analysisErr := compiler.Analyze(path)
	var verdict string
	var verified *compiler.VerifiedProgram
	switch {
	case analysisErr == nil:
		identity := program.Identity()
		report.Identity = &identity
		handle, result, err := compiler.Verify(program)
		verified = handle
		report.EffectCoverage = &checkEffectCoverage{Status: "complete"}
		if !result.EffectsComplete {
			report.EffectCoverage.Status = "incomplete"
		}
		diagnostics = append(result.Violations, result.Incomplete...)
		switch {
		case err == nil:
			verdict = "verified"
		case errors.Is(err, safety.ErrRejected):
			verdict = "rejected"
		case errors.Is(err, safety.ErrIndeterminate):
			verdict = "indeterminate"
		default:
			report.Diagnostics = append(report.Diagnostics, checkFailure(err))
		}
	case len(diagnostics) != 0:
		verdict = "rejected"
	default:
		report.Diagnostics = append(report.Diagnostics, checkFailure(analysisErr))
	}
	if verdict != "" {
		report.Verdict = &verdict
	}

	report.Diagnostics = appendCheckDiagnostics(report.Diagnostics, diagnostics)
	return verified, report
}

func appendCheckDiagnostics(output []checkDiagnostic, diagnostics []diagnostic.Diagnostic) []checkDiagnostic {
	// Canonicalize output order independently of policy pass order.
	slices.SortStableFunc(diagnostics, func(a, b diagnostic.Diagnostic) int {
		return cmp.Or(cmp.Compare(a.Pos.File, b.Pos.File), cmp.Compare(a.Pos.Line, b.Pos.Line),
			cmp.Compare(a.Pos.Column, b.Pos.Column), cmp.Compare(a.Code, b.Code),
			cmp.Compare(a.Message, b.Message), cmp.Compare(a.PolicyRuleID, b.PolicyRuleID))
	})
	for _, diag := range diagnostics {
		output = append(output, projectCheckDiagnostic(diag))
	}
	return output
}

func checkFailure(err error) checkDiagnostic {
	code, phase := "COMPILER_ERROR", "verification"
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		code, phase = "SOURCE_READ_ERROR", "input"
	}
	return checkDiagnostic{Code: code, Phase: phase, Severity: "error", Message: err.Error()}
}

func projectCheckDiagnostic(d diagnostic.Diagnostic) checkDiagnostic {
	var phase string
	switch d.Code {
	case diagnostic.ParseError:
		phase = "parse"
	case diagnostic.TypeError, diagnostic.TypeMismatch:
		phase = "type"
	case diagnostic.DeclaredEffectMissing, diagnostic.InferredEffectExceedsAuthority:
		phase = "effect"
	case diagnostic.PolicyViolation, diagnostic.PolicyCoverageIncomplete:
		phase = "policy"
	}
	result := checkDiagnostic{
		Code: string(d.Code), Phase: phase, Severity: d.Severity.String(),
		Message: d.Message, Source: checkSourceRange(d.Pos, d.End), Effects: d.Effects,
		ExpectedType: d.ExpectedType, ActualType: d.ActualType,
		PolicyRuleID: d.PolicyRuleID, WitnessLength: d.WitnessLength,
	}
	if d.WitnessLength > 0 {
		truncated := d.WitnessTruncated
		result.WitnessTruncated = &truncated
	}
	if len(d.Witness) != 0 {
		result.Witness = make([]checkWitness, 0, len(d.Witness))
	}
	for _, step := range d.Witness {
		kind := "operation"
		declaration := checkSourcePoint(step.Declaration)
		if declaration != nil {
			kind = "call"
		}
		result.Witness = append(result.Witness, checkWitness{
			Kind: kind, Target: step.Target, Source: checkSourceRange(step.Site, step.End), Declaration: declaration,
		})
	}
	return result
}

func checkSourcePoint(position token.Position) *checkPoint {
	if position.Line < 1 || position.Column < 1 {
		return nil
	}
	return &checkPoint{position.File, position.Line, position.Column}
}

func checkSourceRange(start, end token.Position) *checkRange {
	point := checkSourcePoint(start)
	if point == nil {
		return nil
	}
	return &checkRange{Start: *point, End: checkSourcePoint(end)}
}
