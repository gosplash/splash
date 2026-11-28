package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"gosplash.dev/splash/internal/compiler"
)

func runAuthorityCommand(args []string, output io.Writer) (bool, error) {
	if len(args) != 4 || args[0] != "diff" || args[1] == "" || args[2] == "" || args[3] != "--json" {
		return false, errors.New("usage: splash authority diff <base.splash> <head.splash> --json")
	}
	base, baseReport := makeCheckReport(args[1])
	head, headReport := makeCheckReport(args[2])
	report := struct {
		SchemaVersion int                           `json:"schemaVersion"`
		Base          checkReport                   `json:"base"`
		Head          checkReport                   `json:"head"`
		Comparison    *compiler.AuthorityComparison `json:"comparison"`
		Diagnostics   []checkDiagnostic             `json:"diagnostics"`
	}{SchemaVersion: 1, Base: baseReport, Head: headReport, Diagnostics: []checkDiagnostic{}}
	if base != nil && head != nil {
		result, err := compiler.CompareAuthority(base, head)
		if err != nil {
			report.Diagnostics = append(report.Diagnostics, inspectionFailure(err))
		} else {
			report.Comparison = &result
		}
	}
	if err := json.NewEncoder(output).Encode(report); err != nil {
		return false, fmt.Errorf("write authority comparison: %w", err)
	}
	return report.Comparison != nil, nil
}
