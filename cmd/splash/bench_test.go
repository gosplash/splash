package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/safety"
	"gosplash.dev/splash/internal/typechecker"
)

// genCheckSrc generates a Splash program suitable for end-to-end splash check
// benchmarking. It includes a call chain, effect declarations, an approve fn
// declaration, a redline function, and a @sensitive type — exercising all
// safety passes, not just the parser and type checker.
func genCheckSrc(n int) string {
	var b strings.Builder
	b.WriteString("module bench\n\n")

	// Sensitive type — exercises tool fn data classification check
	b.WriteString("type Credential {\n    id: Int\n    @sensitive\n    token: String\n}\n\n")

	// approve fn declaration — exercises approval cascade
	b.WriteString("approve fn sensitive_op() needs DB.write -> Int { return 0 }\n\n")

	// redline function — must not be agent-reachable
	b.WriteString("redline(reason: \"admin only\") fn admin_op() needs DB.admin -> Int { return 0 }\n\n")

	// Call chain with DB.read effect
	b.WriteString("fn f0() needs DB.read -> Int { return 0 }\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, "fn f%d() needs DB.read -> Int { return f%d() }\n", i, i-1)
	}

	// Agent entry — reachable set includes the full chain
	fmt.Fprintf(&b, "\nfn run_agent() needs Agent, DB.read -> Int { return f%d() }\n", n-1)
	return b.String()
}

// checkPipeline runs the full splash check pipeline without printing output.
// This is the benchmarkable equivalent of runCheck.
func checkPipeline(path string) error {
	f, err := parseFile(path)
	if err != nil {
		return err
	}
	tc := typechecker.New()
	tc.SetFileLoader(filepath.Dir(path), os.ReadFile)
	_, typeErrs := tc.Check(f)
	if len(typeErrs) > 0 {
		return fmt.Errorf("type errors")
	}
	g := callgraph.Build(f)
	safetyErrs := safety.New().Check(f, g)
	if len(safetyErrs) > 0 {
		return fmt.Errorf("safety errors")
	}
	return nil
}

// BenchmarkCheck_Pipeline_* measures the full splash check pipeline:
// parse → type check → call graph → safety check.
// This is the hot path for developer feedback during development.

func BenchmarkCheck_Pipeline_100(b *testing.B) {
	path := writeTempSplash(b, genCheckSrc(100))
	b.ResetTimer()
	for range b.N {
		if err := checkPipeline(path); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCheck_Pipeline_500(b *testing.B) {
	path := writeTempSplash(b, genCheckSrc(500))
	b.ResetTimer()
	for range b.N {
		if err := checkPipeline(path); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCheck_Pipeline_2000(b *testing.B) {
	path := writeTempSplash(b, genCheckSrc(2000))
	b.ResetTimer()
	for range b.N {
		if err := checkPipeline(path); err != nil {
			b.Fatal(err)
		}
	}
}

// writeTempSplash writes src to a temp file and returns the path.
// The file is cleaned up when the benchmark completes.
func writeTempSplash(b *testing.B, src string) string {
	b.Helper()
	dir := b.TempDir()
	path := filepath.Join(dir, "bench.splash")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		b.Fatal(err)
	}
	return path
}
