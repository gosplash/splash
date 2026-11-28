package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/compiler"
)

func BenchmarkModuleSummary(b *testing.B) {
	const moduleCount, functionCount = 40, 32
	dir := b.TempDir()
	for module := range moduleCount {
		var source strings.Builder
		fmt.Fprintf(&source, "module feature%d\n", module)
		if module > 0 {
			fmt.Fprintf(&source, "use feature%d\n", module-1)
		}
		fmt.Fprintf(&source, "type Item%d { count: Int }\n", module)
		for fn := range functionCount {
			fmt.Fprintf(&source, "fn calculate%d_%d(item: Item%d) -> Int { return item.count }\n", module, fn, module)
		}
		path := filepath.Join(dir, fmt.Sprintf("feature%d.splash", module))
		if err := os.WriteFile(path, []byte(source.String()), 0600); err != nil {
			b.Fatal(err)
		}
	}
	path := filepath.Join(dir, "feature39.splash")
	program, diagnostics, err := compiler.Analyze(path)
	if err != nil {
		b.Fatalf("analyze: %v, diagnostics: %v", err, diagnostics)
	}
	query := func(b *testing.B) compiler.ModuleSummary {
		result, err := program.SummarizeModule("feature35", compiler.DefaultContextLimit)
		if err != nil || len(result.Declarations) != 20 || result.TotalDeclarations != functionCount+1 {
			b.Fatalf("summary query returned %+v, %v", result, err)
		}
		return result
	}
	result := query(b)
	b.Run("Query", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			query(b)
		}
	})
	b.Run("ProjectAndEncode", func(b *testing.B) {
		var output bytes.Buffer
		encoder := json.NewEncoder(&output)
		identity := program.Identity()
		report := summaryReport{
			SchemaVersion: 1, EntryPath: "feature39.splash", Identity: &identity, Diagnostics: []checkDiagnostic{},
		}

		b.ReportAllocs()
		for b.Loop() {
			output.Reset()
			report.Result = projectModuleSummary(result)
			if err := encoder.Encode(report); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(output.Len()), "output-B")
	})
}
