package callgraph_test

import (
	"fmt"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
)

// genChain generates a linear call chain of n functions:
//
//	f0 → f1 → f2 → ... → fn-1 → run_agent (needs Agent)
//
// Every function is agent-reachable, maximising reachability work.
func genChain(n int) string {
	var b strings.Builder
	b.WriteString("module bench\n\n")
	b.WriteString("fn f0() -> Int { return 0 }\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, "fn f%d() -> Int { return f%d() }\n", i, i-1)
	}
	fmt.Fprintf(&b, "fn run_agent() needs Agent -> Int { return f%d() }\n", n-1)
	return b.String()
}

// genFan generates a fan-out topology: run_agent calls n leaf functions directly.
// Tests wide rather than deep reachability traversal.
func genFan(n int) string {
	var b strings.Builder
	b.WriteString("module bench\n\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "fn f%d() -> Int { return %d }\n", i, i)
	}
	b.WriteString("fn run_agent() needs Agent -> Int {\n")
	b.WriteString("    let x = f0()\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, "    let x%d = f%d()\n", i, i)
	}
	b.WriteString("    return x\n}\n")
	return b.String()
}

func parseBench(src string) *ast.File {
	toks := lexer.New("bench.splash", src).Tokenize()
	p := parser.New("bench.splash", toks)
	f, _ := p.ParseFile()
	return f
}

// BenchmarkBuild measures call graph construction at three program sizes.

func BenchmarkBuild_100(b *testing.B) {
	f := parseBench(genChain(100))
	b.ResetTimer()
	for range b.N {
		callgraph.Build(f)
	}
}

func BenchmarkBuild_1000(b *testing.B) {
	f := parseBench(genChain(1000))
	b.ResetTimer()
	for range b.N {
		callgraph.Build(f)
	}
}

func BenchmarkBuild_5000(b *testing.B) {
	f := parseBench(genChain(5000))
	b.ResetTimer()
	for range b.N {
		callgraph.Build(f)
	}
}

// BenchmarkReachable measures forward BFS from the agent root.

func BenchmarkReachable_100(b *testing.B) {
	g := callgraph.Build(parseBench(genChain(100)))
	roots := g.AgentRoots()
	b.ResetTimer()
	for range b.N {
		g.Reachable(roots)
	}
}

func BenchmarkReachable_1000(b *testing.B) {
	g := callgraph.Build(parseBench(genChain(1000)))
	roots := g.AgentRoots()
	b.ResetTimer()
	for range b.N {
		g.Reachable(roots)
	}
}

func BenchmarkReachable_5000(b *testing.B) {
	g := callgraph.Build(parseBench(genChain(5000)))
	roots := g.AgentRoots()
	b.ResetTimer()
	for range b.N {
		g.Reachable(roots)
	}
}

// BenchmarkCallers measures reverse BFS (used for approve fn cascade).

func BenchmarkCallers_100(b *testing.B) {
	g := callgraph.Build(parseBench(genChain(100)))
	targets := map[string]bool{"f0": true} // deepest leaf — all others are callers
	b.ResetTimer()
	for range b.N {
		g.Callers(targets)
	}
}

func BenchmarkCallers_1000(b *testing.B) {
	g := callgraph.Build(parseBench(genChain(1000)))
	targets := map[string]bool{"f0": true}
	b.ResetTimer()
	for range b.N {
		g.Callers(targets)
	}
}

func BenchmarkCallers_5000(b *testing.B) {
	g := callgraph.Build(parseBench(genChain(5000)))
	targets := map[string]bool{"f0": true}
	b.ResetTimer()
	for range b.N {
		g.Callers(targets)
	}
}

// BenchmarkFan measures reachability on a wide (not deep) topology.

func BenchmarkReachable_Fan_1000(b *testing.B) {
	g := callgraph.Build(parseBench(genFan(1000)))
	roots := g.AgentRoots()
	b.ResetTimer()
	for range b.N {
		g.Reachable(roots)
	}
}
