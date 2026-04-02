package typechecker_test

import (
	"fmt"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
	"gosplash.dev/splash/internal/typechecker"
)

// genFlatFunctions generates n independent functions that return a constant.
// Measures pure type-checking cost without call graph overhead.
func genFlatFunctions(n int) string {
	var b strings.Builder
	b.WriteString("module bench\n\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "fn f%d() -> Int { return %d }\n", i, i)
	}
	return b.String()
}

// genChainWithEffects generates a call chain where each function declares
// an effect, exercising effect propagation checking at every call site.
func genChainWithEffects(n int) string {
	var b strings.Builder
	b.WriteString("module bench\n\n")
	b.WriteString("fn leaf() needs DB.read -> Int { return 0 }\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, "fn f%d() needs DB.read -> Int { return f%d() }\n", i, i-1)
	}
	fmt.Fprintf(&b, "fn run_agent() needs Agent, DB.read -> Int { return f%d() }\n", n-1)
	return b.String()
}

// genMixedTypes generates n type declarations with fields and n functions
// that construct and return those types. Exercises type resolution.
func genMixedTypes(n int) string {
	var b strings.Builder
	b.WriteString("module bench\n\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "type T%d { id: Int; value: String }\n", i)
	}
	b.WriteString("\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "fn make_%d(id: Int) -> T%d { return T%d { id: id, value: \"x\" } }\n", i, i, i)
	}
	return b.String()
}

func parseTCBench(src string) *ast.File {
	toks := lexer.New("bench.splash", src).Tokenize()
	p := parser.New("bench.splash", toks)
	f, _ := p.ParseFile()
	return f
}

// BenchmarkCheck_Flat measures the type checker on programs with many
// independent functions (no effects, no call chain).

func BenchmarkCheck_Flat_100(b *testing.B) {
	f := parseTCBench(genFlatFunctions(100))
	b.ResetTimer()
	for range b.N {
		typechecker.New().Check(f)
	}
}

func BenchmarkCheck_Flat_500(b *testing.B) {
	f := parseTCBench(genFlatFunctions(500))
	b.ResetTimer()
	for range b.N {
		typechecker.New().Check(f)
	}
}

func BenchmarkCheck_Flat_2000(b *testing.B) {
	f := parseTCBench(genFlatFunctions(2000))
	b.ResetTimer()
	for range b.N {
		typechecker.New().Check(f)
	}
}

// BenchmarkCheck_Effects measures the type checker on programs with
// effect declarations at every call site.

func BenchmarkCheck_Effects_100(b *testing.B) {
	f := parseTCBench(genChainWithEffects(100))
	b.ResetTimer()
	for range b.N {
		typechecker.New().Check(f)
	}
}

func BenchmarkCheck_Effects_500(b *testing.B) {
	f := parseTCBench(genChainWithEffects(500))
	b.ResetTimer()
	for range b.N {
		typechecker.New().Check(f)
	}
}

func BenchmarkCheck_Effects_2000(b *testing.B) {
	f := parseTCBench(genChainWithEffects(2000))
	b.ResetTimer()
	for range b.N {
		typechecker.New().Check(f)
	}
}

// BenchmarkCheck_Types measures type resolution cost for programs
// with many named types and struct literal construction.

func BenchmarkCheck_Types_100(b *testing.B) {
	f := parseTCBench(genMixedTypes(100))
	b.ResetTimer()
	for range b.N {
		typechecker.New().Check(f)
	}
}

func BenchmarkCheck_Types_500(b *testing.B) {
	f := parseTCBench(genMixedTypes(500))
	b.ResetTimer()
	for range b.N {
		typechecker.New().Check(f)
	}
}
