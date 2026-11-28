package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/compiler"
)

func BenchmarkDependencies(b *testing.B) {
	chain := writeTempSplash(b, genCheckSrc(2000))
	large := writeTempSplash(b, genCheckSrc(10000))
	modules := writeDependencyModules(b, 40)
	for _, tc := range []struct {
		name, path, query string
		direction         compiler.DependencyDirection
		depth, expected   int
	}{
		{"Chain2000Depth4", chain, "bench.f0", compiler.Callers, 4, 4},
		{"Chain10000Depth4", large, "bench.f4000", compiler.Callers, 4, 4},
		{"MultiModuleFanIn", modules, "shared.finish", compiler.Callers, 0, 80},
	} {
		b.Run(tc.name, func(b *testing.B) {
			program := analyzeDependencyBenchmark(b, tc.path)
			first, err := program.Dependencies(tc.query, tc.direction, tc.depth)
			if err != nil || len(first.Relationships) != tc.expected {
				b.Fatalf("dependency result: %v, %d relationships", err, len(first.Relationships))
			}
			b.ReportAllocs()
			for b.Loop() {
				result, err := program.Dependencies(tc.query, tc.direction, tc.depth)
				if err != nil || len(result.Relationships) != tc.expected {
					b.Fatalf("dependency result: %v, %d relationships", err, len(result.Relationships))
				}
			}
		})
	}
	b.Run("Chain2000FirstQueryAfterAnalysis", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			b.StopTimer()
			program := analyzeDependencyBenchmark(b, chain)
			b.StartTimer()
			result, err := program.Dependencies("bench.f0", compiler.Callers, 4)
			if err != nil || len(result.Relationships) != 4 {
				b.Fatalf("dependency result: %v, %d relationships", err, len(result.Relationships))
			}
		}
	})
	b.Run("MultiModuleJSONEncoding", func(b *testing.B) {
		report := makeDependencyReport(modules, "shared.finish", compiler.Callers, 0)
		if report.Result == nil || len(report.Result.Relationships) != 80 {
			b.Fatalf("unexpected report: %+v", report)
		}
		var output bytes.Buffer
		encoder := json.NewEncoder(&output)
		b.ReportAllocs()
		for b.Loop() {
			output.Reset()
			if err := encoder.Encode(report); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(output.Len()), "output-B")
	})
	b.Run("SmallFreshProcess", func(b *testing.B) {
		bin := buildSplashCLI(b)
		path := writeDependencyModules(b, 2)
		args := []string{"callers", path, "shared.finish", "--json", "--transitive"}
		initial, err := exec.Command(bin, args...).Output()
		if err != nil || !bytes.Contains(initial, []byte(`"symbol":"m1.run"`)) {
			b.Fatalf("unexpected initial output: %s, %v", initial, err)
		}
		for b.Loop() {
			output, err := exec.Command(bin, args...).Output()
			if err != nil || !bytes.Equal(initial, output) {
				b.Fatalf("dependency process: %s, %v", output, err)
			}
		}
		b.ReportMetric(float64(len(initial)), "output-B")
	})
}

func analyzeDependencyBenchmark(b *testing.B, path string) *compiler.AnalyzedProgram {
	b.Helper()
	program, diagnostics, err := compiler.Analyze(path)
	if err != nil {
		b.Fatalf("analyze: %v, diagnostics %v", err, diagnostics)
	}
	return program
}

func writeDependencyModules(b *testing.B, count int) string {
	b.Helper()
	dir := b.TempDir()
	write := func(name, source string) {
		b.Helper()
		if err := os.WriteFile(filepath.Join(dir, name+".splash"), []byte(source), 0600); err != nil {
			b.Fatal(err)
		}
	}

	write("shared", "module shared\nfn finish() {}\n")
	var entry strings.Builder
	entry.WriteString("module bench\n")
	for i := range count {
		name := fmt.Sprintf("m%d", i)
		write(name, fmt.Sprintf("module %s\nuse shared as common\nfn bridge() { common.finish() }\nfn run() { bridge() }\n", name))
		fmt.Fprintf(&entry, "use %s as %s\n", name, name)
	}
	entry.WriteString("fn main() {}\n")
	write("main", entry.String())
	return filepath.Join(dir, "main.splash")
}
