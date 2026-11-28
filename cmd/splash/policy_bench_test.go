package main

import (
	"fmt"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/compiler"
)

// Each sandbox reaches the same diamond chain and approval boundary. Alternate
// sandbox entries are agent roots. The shared leaf is also approval-gated,
// so the reverse approval closure covers the diamonds and every sandbox.
// The source has complete required coverage and passes policy.
func overlappingPolicySource(roots, layers int) string {
	var src strings.Builder
	src.WriteString("module bench\napprove fn tail() needs DB.read -> Int { return 1 }\n")
	next := "tail"
	for i := range layers {
		fmt.Fprintf(&src, "fn left%d() needs DB.read -> Int { return %s() }\n", i, next)
		fmt.Fprintf(&src, "fn right%d() needs DB.read -> Int { return %s() }\n", i, next)
		fmt.Fprintf(&src, "fn join%d() needs DB.read -> Int { left%d()\nreturn right%d() }\n", i, i, i)
		next = fmt.Sprintf("join%d", i)
	}
	for i := range roots {
		fmt.Fprintf(&src, "approve fn approve%d() needs DB.read -> Int { return %s() }\n", i, next)
		src.WriteString("@sandbox(allow: [DB.read], deny: [Net])\n")
		if i%2 == 0 {
			src.WriteString("agent ")
		}
		fmt.Fprintf(&src, "fn root%d() needs DB.read -> Int { return approve%d() }\n", i, i)
	}
	return src.String()
}

func BenchmarkOverlappingPolicyRoots(b *testing.B) {
	for _, shape := range []struct{ roots, layers int }{{16, 32}, {64, 32}, {64, 128}, {256, 128}} {
		b.Run(fmt.Sprintf("Roots%d/Layers%d", shape.roots, shape.layers), func(b *testing.B) {
			path := writeTempSplash(b, overlappingPolicySource(shape.roots, shape.layers))
			program, diagnostics, err := compiler.Analyze(path)
			if err != nil {
				b.Fatalf("analyze benchmark fixture: %v, %v", err, diagnostics)
			}
			b.ReportAllocs()
			for b.Loop() {
				verified, result, err := compiler.Verify(program)
				if err != nil || verified == nil || len(result.Violations) != 0 || len(result.Incomplete) != 0 {
					b.Fatalf("verify benchmark fixture: %v, %+v", err, result)
				}
			}
		})
	}
}

// Count JSON bytes without retaining or printing the report.
type outputByteCount int

func (count *outputByteCount) Write(p []byte) (int, error) {
	*count += outputByteCount(len(p))
	return len(p), nil
}

func BenchmarkRejectedPolicyChain(b *testing.B) {
	// Probe a small input first so a regression cannot turn the large cases
	// into quadratic JSON encodes. The encoder builds its report before Write.
	probe := writeTempSplash(b, rejectedPolicyChain(100))
	var probeBytes outputByteCount
	if ok, err := runCheckJSON(probe, &probeBytes); ok || err != nil || probeBytes > 1<<20 {
		b.Fatalf("bounded rejection probe: verified=%v, err=%v, bytes=%d", ok, err, probeBytes)
	}
	for _, n := range []int{100, 1000, 2000, 10000} {
		b.Run(fmt.Sprintf("Functions%d", n), func(b *testing.B) {
			path := writeTempSplash(b, rejectedPolicyChain(n))
			var output outputByteCount
			b.ReportAllocs()
			for b.Loop() {
				output = 0
				if ok, err := runCheckJSON(path, &output); ok || err != nil {
					b.Fatalf("expected rejected check: %v, %v", ok, err)
				}
			}
			if output > outputByteCount(n*8192) {
				b.Fatalf("rejection exceeded output budget: %d bytes for %d functions", output, n)
			}
			b.ReportMetric(float64(output), "output-B")
		})
	}
}
