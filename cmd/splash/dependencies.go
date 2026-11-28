package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"gosplash.dev/splash/internal/compiler"
)

type dependencyReport struct {
	SchemaVersion int                          `json:"schemaVersion"`
	EntryPath     string                       `json:"entryPath"`
	Query         string                       `json:"query"`
	Direction     compiler.DependencyDirection `json:"direction"`
	Identity      *compiler.AnalysisIdentity   `json:"identity,omitempty"`
	Result        *dependencyProjection        `json:"result"`
	Diagnostics   []checkDiagnostic            `json:"diagnostics"`
}

type dependencyProjection struct {
	Root          dependencyFunction       `json:"root"`
	MaxDepth      *int                     `json:"maxDepth"`
	Relationships []dependencyRelationship `json:"relationships"`
	Coverage      dependencyCoverage       `json:"coverage"`
}

type dependencyFunction struct {
	Symbol      string      `json:"symbol"`
	Declaration *checkPoint `json:"declaration"`
}

type dependencyRelationship struct {
	Function dependencyFunction `json:"function"`
	Via      dependencyFunction `json:"via"`
	Distance int                `json:"distance"`
	Source   *checkRange        `json:"source"`
}

type dependencyCoverage struct {
	Scope  string         `json:"scope"`
	Status string         `json:"status"`
	Gap    *dependencyGap `json:"gap,omitempty"`
}

type dependencyGap struct {
	Function dependencyFunction `json:"function"`
	Reason   string             `json:"reason"`
	Source   *checkRange        `json:"source,omitempty"`
}

func runDependencyCommand(direction compiler.DependencyDirection, args []string, output io.Writer) (bool, error) {
	if len(args) < 3 || args[0] == "" || args[1] == "" || args[2] != "--json" {
		return false, dependencyUsage(direction)
	}
	depth := 1
	switch {
	case len(args) == 3:
	case len(args) == 4 && args[3] == "--transitive":
		depth = 0
	case len(args) == 5 && args[3] == "--depth":
		var err error
		depth, err = strconv.Atoi(args[4])
		if err != nil || depth < 1 {
			return false, fmt.Errorf("--depth requires a positive integer")
		}
	default:
		return false, dependencyUsage(direction)
	}

	report := makeDependencyReport(args[0], args[1], direction, depth)
	if err := json.NewEncoder(output).Encode(report); err != nil {
		return false, fmt.Errorf("write dependency report: %w", err)
	}
	return report.Result != nil, nil
}

func dependencyUsage(direction compiler.DependencyDirection) error {
	return fmt.Errorf("usage: splash %s <file.splash> <module.function> --json [--transitive | --depth N]", direction)
}

func makeDependencyReport(path, query string, direction compiler.DependencyDirection, depth int) dependencyReport {
	program, diagnostics := analyzeForInspection(path)
	report := dependencyReport{
		SchemaVersion: 1, EntryPath: path, Query: query, Direction: direction, Diagnostics: diagnostics,
	}
	if program == nil {
		return report
	}
	identity := program.Identity()
	report.Identity = &identity
	result, err := program.Dependencies(query, direction, depth)
	if err != nil {
		report.Diagnostics = append(report.Diagnostics, inspectionFailure(err))
		return report
	}
	report.Result = projectDependencies(result, direction, depth)
	return report
}

func projectDependencies(result compiler.DependencyResult, direction compiler.DependencyDirection, depth int) *dependencyProjection {
	out := &dependencyProjection{
		Root:          projectDependencyFunction(result.Root),
		Relationships: make([]dependencyRelationship, 0, len(result.Relationships)),
		Coverage:      dependencyCoverage{Scope: "expandedFunctions", Status: "complete"},
	}
	if depth != 0 {
		out.MaxDepth = &depth
	}
	if direction == compiler.Callers {
		out.Coverage.Scope = "entryClosure"
	}
	for _, relationship := range result.Relationships {
		out.Relationships = append(out.Relationships, dependencyRelationship{
			Function: projectDependencyFunction(relationship.Function), Via: projectDependencyFunction(relationship.Via),
			Distance: relationship.Distance, Source: checkSourceRange(relationship.Position, relationship.End),
		})
	}
	if gap := result.Gap; gap != nil {
		out.Coverage.Status = "incomplete"
		out.Coverage.Gap = &dependencyGap{
			Function: projectDependencyFunction(gap.Function), Reason: gap.Reason, Source: checkSourceRange(gap.Position, gap.End),
		}
	}
	return out
}

func projectDependencyFunction(fn compiler.DependencyFunction) dependencyFunction {
	return dependencyFunction{Symbol: fn.Symbol, Declaration: checkSourcePoint(fn.Declaration)}
}
