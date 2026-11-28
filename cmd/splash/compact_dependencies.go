package main

import (
	"fmt"
	"strconv"

	"gosplash.dev/splash/internal/compiler"
)

const (
	defaultContextLimit = 20
	maxContextLimit     = 100
)

type compactDependencyProjection struct {
	Root          compiler.CompactDependencyFunction `json:"root"`
	MaxDepth      *int                               `json:"maxDepth"`
	Matches       []compiler.CompactDependencyMatch  `json:"matches"`
	Limit         int                                `json:"limit"`
	Total         int                                `json:"total"`
	Omitted       int                                `json:"omitted"`
	OmittedFields []string                           `json:"omittedFields"`
	Coverage      dependencyCoverage                 `json:"coverage"`
}

func compactDependencyOptions(args []string) (depth, limit int, err error) {
	depth, limit = 1, defaultContextLimit
	depthSet, limitSet := false, false
	for len(args) > 0 {
		option := args[0]
		args = args[1:]
		switch option {
		case "--limit":
			if limitSet || len(args) == 0 {
				return 0, 0, fmt.Errorf("expected a single --limit value")
			}
			limit, err = strconv.Atoi(args[0])
			if err != nil || limit < 1 || limit > maxContextLimit {
				return 0, 0, fmt.Errorf("--limit requires an integer from 1 to %d", maxContextLimit)
			}
			args, limitSet = args[1:], true
		case "--transitive":
			if depthSet {
				return 0, 0, fmt.Errorf("use only one of --depth or --transitive")
			}
			depth, depthSet = 0, true
		case "--depth":
			if depthSet || len(args) == 0 {
				return 0, 0, fmt.Errorf("expected a single --depth value or --transitive")
			}
			depth, err = strconv.Atoi(args[0])
			if err != nil || depth < 1 {
				return 0, 0, fmt.Errorf("--depth requires a positive integer")
			}
			args, depthSet = args[1:], true
		default:
			return 0, 0, fmt.Errorf("unknown compact dependency option %q", option)
		}
	}
	return depth, limit, nil
}

func makeCompactDependencyReport(path, query string, direction compiler.DependencyDirection, depth, limit int) dependencyReport[compactDependencyProjection] {
	program, diagnostics := analyzeForInspection(path)
	report := dependencyReport[compactDependencyProjection]{
		SchemaVersion: 1, EntryPath: path, Query: query, Direction: direction, View: "compact", Diagnostics: diagnostics,
	}
	if program == nil {
		return report
	}
	identity := program.Identity()
	report.Identity = &identity
	result, err := program.CompactDependencies(query, direction, depth, limit)
	if err != nil {
		report.Diagnostics = append(report.Diagnostics, inspectionFailure(err))
		return report
	}
	report.Result = projectCompactDependencies(result, direction, depth, limit)
	return report
}

func projectCompactDependencies(result compiler.CompactDependencyResult, direction compiler.DependencyDirection, depth, limit int) *compactDependencyProjection {
	out := &compactDependencyProjection{
		Root: result.Root, Matches: result.Matches,
		Limit: limit, Total: result.Total, Omitted: result.Total - len(result.Matches),
		OmittedFields: []string{"via", "source"},
		Coverage:      dependencyCoverageFor(direction, result.Gap),
	}
	if depth != 0 {
		out.MaxDepth = &depth
	}
	return out
}
