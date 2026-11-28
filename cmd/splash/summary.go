package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gosplash.dev/splash/internal/compiler"
	"gosplash.dev/splash/internal/token"
)

type summaryReport struct {
	SchemaVersion int                        `json:"schemaVersion"`
	EntryPath     string                     `json:"entryPath"`
	Identity      *compiler.AnalysisIdentity `json:"identity,omitempty"`
	Result        *summaryResult             `json:"result"`
	Diagnostics   []checkDiagnostic          `json:"diagnostics"`
}

type summaryResult struct {
	Module              string               `json:"module"`
	SourcePath          string               `json:"sourcePath"`
	Declarations        []summaryDeclaration `json:"declarations"`
	TotalDeclarations   int                  `json:"totalDeclarations"`
	OmittedDeclarations int                  `json:"omittedDeclarations"`
}

type summaryDeclaration struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	*summaryFunction
}

type summaryFunction struct {
	Signature         compiler.Signature    `json:"signature"`
	DeclaredAuthority []string              `json:"declaredAuthority"`
	InferredEffects   []string              `json:"inferredEffects"`
	EffectCoverage    summaryEffectCoverage `json:"effectCoverage"`
}

type summaryEffectCoverage struct {
	Status string      `json:"status"`
	Gap    *summaryGap `json:"gap,omitempty"`
}

type summaryGap struct {
	Reason string        `json:"reason"`
	Source *summaryRange `json:"source,omitempty"`
}

type summaryRange struct {
	Start *summaryPoint `json:"start"`
	End   *summaryPoint `json:"end,omitempty"`
}

type summaryPoint struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

func runSummaryCommand(args []string, output io.Writer) (bool, error) {
	usage := errors.New("usage: splash summary <entry.splash> --json [--module NAME] [--limit N]")
	if len(args) < 2 || args[0] == "" || strings.HasPrefix(args[0], "--") || args[1] != "--json" {
		return false, usage
	}
	var module string
	var limit int
	for options := args[2:]; len(options) != 0; options = options[2:] {
		if len(options) < 2 {
			return false, usage
		}
		option, value := options[0], options[1]
		switch option {
		case "--module":
			if module != "" || value == "" || strings.HasPrefix(value, "-") {
				return false, usage
			}
			module = value
		case "--limit":
			if limit != 0 {
				return false, usage
			}
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > compiler.MaxModuleSummaryLimit {
				return false, fmt.Errorf("--limit requires an integer between 1 and %d\n%w", compiler.MaxModuleSummaryLimit, usage)
			}
			limit = n
		default:
			return false, usage
		}
	}
	if limit == 0 {
		limit = compiler.DefaultModuleSummaryLimit
	}

	report := summaryReport{SchemaVersion: 1, EntryPath: args[0]}
	program, diagnostics := analyzeForInspection(report.EntryPath)
	report.Diagnostics = diagnostics
	if program != nil {
		identity := program.Identity()
		report.Identity = &identity
		result, err := program.SummarizeModule(module, limit)
		if err == nil {
			report.Result = projectModuleSummary(result)
		} else {
			diag := inspectionFailure(err)
			switch {
			case errors.Is(err, compiler.ErrModuleNotFound):
				diag.Code = "MODULE_NOT_FOUND"
			case errors.Is(err, compiler.ErrAmbiguousModule):
				diag.Code = "MODULE_AMBIGUOUS"
			}
			report.Diagnostics = append(report.Diagnostics, diag)
		}
	}
	if err := json.NewEncoder(output).Encode(report); err != nil {
		return false, fmt.Errorf("write summary report: %w", err)
	}
	return report.Result != nil, nil
}

func projectModuleSummary(summary compiler.ModuleSummary) *summaryResult {
	result := &summaryResult{
		Module: summary.Module, SourcePath: summary.SourcePath,
		TotalDeclarations: summary.TotalDeclarations, OmittedDeclarations: summary.OmittedDeclarations,
		Declarations: make([]summaryDeclaration, 0, len(summary.Declarations)),
	}
	for _, decl := range summary.Declarations {
		item := summaryDeclaration{Kind: decl.Kind, Name: decl.Name, Line: decl.Line, Column: decl.Column}
		if contract := decl.Function; contract != nil {
			item.summaryFunction = &summaryFunction{
				Signature: contract.Signature, DeclaredAuthority: contract.Declared.Labels(),
				InferredEffects: contract.Inferred.Labels(), EffectCoverage: summaryEffectCoverage{Status: "complete"},
			}
			if gap := contract.Gap; gap != nil {
				coverage := &item.summaryFunction.EffectCoverage
				coverage.Status = "incomplete"
				coverage.Gap = &summaryGap{Reason: gap.Reason}
				if start := summarySourcePoint(gap.Position, summary.SourcePath); start != nil {
					coverage.Gap.Source = &summaryRange{Start: start, End: summarySourcePoint(gap.End, summary.SourcePath)}
				}
			}
		}
		result.Declarations = append(result.Declarations, item)
	}
	return result
}

func summarySourcePoint(pos token.Position, modulePath string) *summaryPoint {
	if pos.Line < 1 || pos.Column < 1 {
		return nil
	}
	path := pos.File
	if path == modulePath {
		path = ""
	}
	return &summaryPoint{File: path, Line: pos.Line, Column: pos.Column}
}
