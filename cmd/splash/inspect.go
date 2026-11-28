package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"gosplash.dev/splash/internal/compiler"
)

type inspectReport struct {
	SchemaVersion int               `json:"schemaVersion"`
	EntryPath     string            `json:"entryPath"`
	Query         string            `json:"query"`
	Contract      *inspectContract  `json:"contract"`
	Diagnostics   []checkDiagnostic `json:"diagnostics"`
}

type inspectContract struct {
	Symbol            string             `json:"symbol"`
	Declaration       *checkPoint        `json:"declaration"`
	Signature         compiler.Signature `json:"signature"`
	DeclaredAuthority []string           `json:"declaredAuthority"`
	InferredEffects   []string           `json:"inferredEffects"`
	EffectCoverage    inspectCoverage    `json:"effectCoverage"`
}

type inspectCoverage struct {
	Status string      `json:"status"`
	Gap    *inspectGap `json:"gap,omitempty"`
}

type inspectGap struct {
	Reason string      `json:"reason"`
	Source *checkRange `json:"source,omitempty"`
}

func runInspectCommand(args []string, output io.Writer) (bool, error) {
	if len(args) != 3 || args[0] == "" || args[1] == "" || args[2] != "--json" {
		return false, errors.New("usage: splash inspect <file.splash> <module.function> --json")
	}
	report := makeInspectReport(args[0], args[1])
	if err := json.NewEncoder(output).Encode(report); err != nil {
		return false, fmt.Errorf("write inspect report: %w", err)
	}
	return report.Contract != nil, nil
}

func makeInspectReport(path, query string) inspectReport {
	report := inspectReport{SchemaVersion: 1, EntryPath: path, Query: query, Diagnostics: []checkDiagnostic{}}
	program, diagnostics, err := compiler.Analyze(path)
	if err != nil {
		if len(diagnostics) == 0 {
			report.Diagnostics = append(report.Diagnostics, checkFailure(err))
		}
		report.Diagnostics = appendCheckDiagnostics(report.Diagnostics, diagnostics)
		return report
	}
	contract, err := program.InspectFunction(query)
	if err != nil {
		code := "COMPILER_ERROR"
		switch {
		case errors.Is(err, compiler.ErrFunctionNotFound):
			code = "FUNCTION_NOT_FOUND"
		case errors.Is(err, compiler.ErrAmbiguousFunction):
			code = "FUNCTION_AMBIGUOUS"
		}
		report.Diagnostics = append(report.Diagnostics, checkDiagnostic{
			Code: code, Phase: "inspection", Severity: "error", Message: err.Error(),
		})
		return report
	}

	report.Contract = projectInspectContract(contract)
	return report
}

func projectInspectContract(contract compiler.FunctionContract) *inspectContract {
	projection := &inspectContract{
		Symbol: contract.Symbol, Declaration: checkSourcePoint(contract.Declaration), Signature: contract.Signature,
		DeclaredAuthority: contract.Declared.Labels(), InferredEffects: contract.Inferred.Labels(),
		EffectCoverage: inspectCoverage{Status: "complete"},
	}
	if gap := contract.Gap; gap != nil {
		projection.EffectCoverage = inspectCoverage{
			Status: "incomplete",
			Gap:    &inspectGap{Reason: gap.Reason, Source: checkSourceRange(gap.Position, gap.End)},
		}
	}
	return projection
}
