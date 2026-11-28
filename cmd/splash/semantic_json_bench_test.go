package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/compiler"
)

// Both fixtures exercise the same analyzed declarations, policies and queries.
// Line comments vary the captured byte count while keeping output sizes equal.
func semanticJSONSources() []struct{ name, source string } {
	source := genCheckSrc(2000)
	padding := strings.Repeat("// "+strings.Repeat("x", 1020)+"\n", 2048)
	return []struct{ name, source string }{
		{"original", source},
		{"padded", source + padding},
	}
}

// BenchmarkSemanticJSONCLI times fresh processes with warm OS caches and a
// primed binary. CLI build and expected-response validation are untimed.
func BenchmarkSemanticJSONCLI(b *testing.B) {
	binary := buildSplashCLI(b)
	for _, fixture := range semanticJSONSources() {
		path := writeTempSplash(b, fixture.source)
		for _, command := range []struct {
			name string
			args []string
			want []byte
		}{
			{"check", []string{"check", path, "--json"}, []byte(`"verdict":"verified"`)},
			{"inspect", []string{"inspect", path, "bench.f1000", "--json"}, []byte(`"symbol":"bench.f1000"`)},
			{"callers", []string{"callers", path, "bench.f1000", "--json", "--depth", "4"}, []byte(`"symbol":"bench.f1004"`)},
		} {
			b.Run(fixture.name+"/"+command.name, func(b *testing.B) {
				expected, err := exec.Command(binary, command.args...).Output()
				if err != nil {
					b.Fatal(err)
				}
				var response struct{ Identity compiler.AnalysisIdentity }
				if err := json.Unmarshal(expected, &response); err != nil ||
					response.Identity.AnalysisID == "" || response.Identity.SourceID == "" ||
					!bytes.Contains(expected, command.want) {
					b.Fatalf("unexpected digest-enabled report: %s (%v)", expected, err)
				}
				for b.Loop() {
					actual, err := exec.Command(binary, command.args...).Output()
					if err != nil || !bytes.Equal(actual, expected) {
						b.Fatalf("unexpected CLI result: %s (%v)", actual, err)
					}
				}
				b.ReportMetric(float64(len(fixture.source)), "source-B")
				b.ReportMetric(float64(len(expected)), "json-B")
			})
		}
	}
}

// BenchmarkCapturedIdentity times the first digest for a captured program.
// Source loading and analysis are outside the timed region.
func BenchmarkCapturedIdentity(b *testing.B) {
	for _, fixture := range semanticJSONSources() {
		path := writeTempSplash(b, fixture.source)
		b.Run(fixture.name, func(b *testing.B) {
			for b.Loop() {
				b.StopTimer()
				program, _, err := compiler.Analyze(path)
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				identity := program.Identity()
				if identity.AnalysisID == "" || identity.SourceID == "" {
					b.Fatal("missing capture identity")
				}
			}
			b.ReportMetric(float64(len(fixture.source)), "source-B")
		})
	}
}
