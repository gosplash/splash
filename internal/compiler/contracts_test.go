package compiler_test

import (
	"errors"
	"reflect"
	"testing"

	"gosplash.dev/splash/internal/compiler"
	"gosplash.dev/splash/internal/effects"
	"gosplash.dev/splash/internal/safety"
)

func TestInspectFunctionUsesCapturedImmutableAnalysis(t *testing.T) {
	dir := t.TempDir()
	entry := writeSource(t, dir, "main.splash", "module main\nuse lib as local\nfn main() {}\n")
	writeSource(t, dir, "lib.splash", `module library
async fn inspect<T: Loggable>(value: T, cb: fn() -> Int, limit: Int = 1, ...items: Int) needs DB.read -> Int {
    return cb()
}
`)
	program := analyze(t, entry)
	writeSource(t, dir, "lib.splash", "module library\nfn inspect(value: String) -> String { return value }\n")

	contract, err := program.InspectFunction("library.inspect")
	if err != nil {
		t.Fatal(err)
	}
	signature := contract.Signature
	if !signature.Async || signature.ReturnType != "Int" || len(signature.Parameters) != 4 ||
		signature.Parameters[0].Type != "T" || signature.Parameters[1].Type != "() -> Int" ||
		signature.Parameters[0].TypeIdentity.Kind != "typeParameter" || signature.Parameters[0].TypeIdentity.Name != "T" ||
		signature.Parameters[1].TypeIdentity.ReturnType == nil || signature.Parameters[1].TypeIdentity.ReturnType.Name != "Int" ||
		!signature.Parameters[2].HasDefault || !signature.Parameters[3].Variadic ||
		len(signature.TypeParameters) != 1 || !reflect.DeepEqual(signature.TypeParameters[0].Constraints, []string{"Loggable"}) {
		t.Fatalf("wrong captured signature: %+v", signature)
	}
	if contract.Declared != effects.DBRead || contract.Inferred != effects.None || contract.Gap == nil {
		t.Fatalf("wrong captured effects: %+v", contract)
	}
	if _, result, err := compiler.Verify(program); !errors.Is(err, safety.ErrIndeterminate) ||
		len(result.Incomplete) != 1 || result.Incomplete[0].Pos != contract.Gap.Position {
		t.Fatalf("verification disagrees with the inspected effect gap: %v, %+v", err, result)
	}
	contract.Signature.Parameters[0].Type = "changed"
	contract.Signature.Parameters[1].TypeIdentity.ReturnType.Name = "changed"
	contract.Signature.TypeParameters[0].Constraints[0] = "changed"
	contract.Gap.Reason = "changed"
	again, err := program.InspectFunction("library.inspect")
	if err != nil || again.Signature.Parameters[0].Type != "T" ||
		again.Signature.Parameters[1].TypeIdentity.ReturnType.Name != "Int" ||
		again.Signature.TypeParameters[0].Constraints[0] != "Loggable" || again.Gap.Reason != "unresolved call target" {
		t.Fatalf("returned contract changed the analysis: %v, %+v", err, again)
	}
	fresh, err := analyze(t, entry).InspectFunction("library.inspect")
	if err != nil || fresh.Signature.ReturnType != "String" || fresh.Declared != effects.None || fresh.Gap != nil {
		t.Fatalf("fresh inspection did not see the updated source: %v, %+v", err, fresh)
	}
}

func TestInspectFunctionWithoutModuleDeclaration(t *testing.T) {
	dir := t.TempDir()
	entry := writeSource(t, dir, "main.splash", "fn main() {}\n")
	program := analyze(t, entry)
	contract, err := program.InspectFunction("main")
	if err != nil || contract.Symbol != "main" || contract.Declaration.File != entry ||
		contract.Signature.ReturnType != "Void" {
		t.Fatalf("unexpected bare contract: %+v, %v", contract, err)
	}
	if _, err := program.InspectFunction(".main"); !errors.Is(err, compiler.ErrFunctionNotFound) {
		t.Fatalf("empty qualifier must not look up a module: %v", err)
	}
}

func TestInspectFunctionUsesCallableSignature(t *testing.T) {
	dir := t.TempDir()
	entry := writeSource(t, dir, "main.splash", `module main
type Item { quantity: Int }
fn Item(x: Item) {}
type Batch { count: Int }
fn Batch<T: Loggable>(x: Batch, xs: List<T>, maybe: T?, cb: fn(T) -> T) -> T? {
    return maybe
}
fn main() { Item(Item { quantity: 3 }) }
`)
	program := analyze(t, entry)
	verify(t, program)

	for _, tc := range []struct {
		name   string
		params []string
		result string
	}{
		{"main.Item", []string{"Item"}, "Void"},
		{"main.Batch", []string{"Batch", "List<T>", "T?", "(T) -> T"}, "T?"},
	} {
		contract, err := program.InspectFunction(tc.name)
		if err != nil {
			t.Fatal(err)
		}
		var types []string
		for _, param := range contract.Signature.Parameters {
			types = append(types, param.Type)
		}
		if !reflect.DeepEqual(types, tc.params) || contract.Signature.ReturnType != tc.result {
			t.Errorf("%s: contract got %v -> %s, want %v -> %s", tc.name,
				types, contract.Signature.ReturnType, tc.params, tc.result)
		}
	}
}
