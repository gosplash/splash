package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"gosplash.dev/splash/internal/compiler"
)

func BenchmarkInspect(b *testing.B) {
	dir := b.TempDir()
	invoice, err := os.ReadFile("../../benchmarks/ttcs/invoice/oracle/testdata/splash/invoice.splash")
	if err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "invoice.splash"), invoice, 0600); err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(dir, "app.splash")
	if err := os.WriteFile(path, []byte("module app\nuse invoice as billing\nfn main() { billing.invoice_totals(2, 10, 5) }\n"), 0600); err != nil {
		b.Fatal(err)
	}
	const query = "invoice.invoice_totals"

	small := makeInspectReport(path, query)
	if small.Contract == nil {
		b.Fatalf("benchmark contract unavailable: %+v", small.Diagnostics)
	}

	b.Run("LookupInvoice", func(b *testing.B) { benchmarkInspectLookup(b, path, query) })
	for _, n := range []int{2000, 10000} {
		b.Run(fmt.Sprintf("LookupLastOf%d", n), func(b *testing.B) {
			chain := writeTempSplash(b, genCheckSrc(n))
			benchmarkInspectLookup(b, chain, fmt.Sprintf("bench.f%d", n-1))
		})
	}
	b.Run("JSONEncodeInvoice", func(b *testing.B) {
		var output bytes.Buffer
		encoder := json.NewEncoder(&output)
		b.ReportAllocs()
		for b.Loop() {
			output.Reset()
			if err := encoder.Encode(small); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(output.Len()), "output-B")
	})
	b.Run("FreshProcessInvoice", func(b *testing.B) {
		binary := buildSplashCLI(b)
		invoke := func() []byte {
			output, err := exec.Command(binary, "inspect", path, query, "--json").Output()
			if err != nil {
				b.Fatalf("inspect process: %v, output %q", err, output)
			}
			return output
		}
		start := time.Now()
		initial := invoke()
		firstLaunch := time.Since(start)
		if !bytes.Contains(initial, []byte(`"symbol":"invoice.invoice_totals"`)) {
			b.Fatalf("unexpected inspect output: %s", initial)
		}

		b.ReportAllocs()
		for b.Loop() {
			if output := invoke(); !bytes.Equal(output, initial) {
				b.Fatalf("inspect output changed: %s", output)
			}
		}
		b.ReportMetric(float64(firstLaunch.Nanoseconds()), "first-launch-ns")
		b.ReportMetric(float64(len(initial)), "output-B")
	})
}

func benchmarkInspectLookup(b *testing.B, path, query string) {
	program, diagnostics, err := compiler.Analyze(path)
	if err != nil {
		b.Fatalf("analyze: %v, diagnostics %v", err, diagnostics)
	}
	b.ReportAllocs()
	for b.Loop() {
		contract, err := program.InspectFunction(query)
		if err != nil || contract.Symbol != query {
			b.Fatalf("inspect %q: %v, contract %+v", query, err, contract)
		}
	}
}

func BenchmarkCheckDiagnosticFeedback(b *testing.B) {
	path := writeTempSplash(b, `module service
fn read() needs DB.read -> Int { return 1 }
agent fn report(cb: fn() -> Int) -> Int {
    return cb() + read()
}
`)
	binary := buildSplashCLI(b)
	for _, tc := range []struct {
		mode string
		args []string
	}{{"Text", []string{"check", path}}, {"JSON", []string{"check", path, "--json"}}} {
		b.Run(tc.mode, func(b *testing.B) {
			invoke := func() []byte {
				output, err := exec.Command(binary, tc.args...).CombinedOutput()
				var failure *exec.ExitError
				if !errors.As(err, &failure) || failure.ExitCode() != 1 {
					b.Fatalf("expected diagnostic rejection, got %v and %q", err, output)
				}
				return output
			}
			initial := invoke()
			if !bytes.Contains(initial, []byte("missing declared effect DB.read")) ||
				!bytes.Contains(initial, []byte("unresolved call target")) {
				b.Fatalf("expected both findings: %q", initial)
			}
			for b.Loop() {
				if output := invoke(); !bytes.Equal(output, initial) {
					b.Fatalf("check output changed: %s", output)
				}
			}
			b.ReportMetric(float64(len(initial)), "output-B")
		})
	}
}
