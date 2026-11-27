package vm

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"gosplash.dev/splash/internal/bytecode"
	"gosplash.dev/splash/internal/ir"
	"gosplash.dev/splash/internal/token"
)

func TestRecursiveCallIsolatesFrames(t *testing.T) {
	// Pushing n before calling factorial recursively leaves it on the
	// caller's stack. Each return must restore that frame's operand stack.
	program := bytecode.Program{Entry: 0, Functions: []bytecode.Function{
		{Result: ir.Void, Code: []bytecode.Instruction{
			{Op: bytecode.Const, Kind: ir.Int, Int: 5}, {Op: bytecode.Call, Index: 1}, {Op: bytecode.Print}, {Op: bytecode.Return},
		}},
		{Params: []ir.Kind{ir.Int}, Locals: []ir.Kind{ir.Int}, Result: ir.Int, Code: []bytecode.Instruction{
			{Op: bytecode.Load}, {Op: bytecode.Const, Kind: ir.Int, Int: 1}, {Op: bytecode.Le},
			{Op: bytecode.JumpIfFalse, Index: 6}, {Op: bytecode.Const, Kind: ir.Int, Int: 1}, {Op: bytecode.Return},
			{Op: bytecode.Load}, {Op: bytecode.Load}, {Op: bytecode.Const, Kind: ir.Int, Int: 1},
			{Op: bytecode.Sub}, {Op: bytecode.Call, Index: 1}, {Op: bytecode.Mul}, {Op: bytecode.Return},
		}},
	}}
	var out bytes.Buffer
	runtime, err := New(program, &out, Limits{Instructions: 200, Frames: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.RunMain(); err != nil || out.String() != "120\n" {
		t.Fatalf("RunMain returned %v and wrote %q", err, out.String())
	}
	for _, test := range []struct{ input, want int64 }{{0, 1}, {5, 120}, {6, 720}} {
		got, err := runtime.Invoke(1, []Value{{Kind: ir.Int, Int: test.input}})
		if err != nil || got != (Value{Kind: ir.Int, Int: test.want}) {
			t.Fatalf("factorial(%d) returned %v, %v", test.input, got, err)
		}
	}
	_, err = runtime.Invoke(1, []Value{{Kind: ir.Bool}})
	requireError(t, err, InvalidInvocation, 0, 0, token.Position{})
}

func TestInvokeRequiresUTF8InStringArguments(t *testing.T) {
	pos := token.Position{File: "strings.splash", Line: 1, Column: 1}
	program := bytecode.Program{Entry: -1, Functions: []bytecode.Function{{
		Params: []ir.Kind{ir.String}, Locals: []ir.Kind{ir.String}, Result: ir.String, Source: pos,
		Code: []bytecode.Instruction{{Op: bytecode.Load}, {Op: bytecode.Return}},
	}}}
	runtime, err := New(program, nil, Limits{Instructions: 2, Frames: 1})
	if err != nil {
		t.Fatal(err)
	}
	value := Value{Kind: ir.String, Text: "水🌊"}
	if got, err := runtime.Invoke(0, []Value{value}); err != nil || got != value {
		t.Fatalf("want Unicode roundtrip, got %v, %v", got, err)
	}
	value.Text = "\xff"
	_, err = runtime.Invoke(0, []Value{value})
	requireError(t, err, InvalidInvocation, 0, 0, pos)
}

func TestRecordValuesAreImmutableAndRuntimeOwned(t *testing.T) {
	kind := ir.RecordBase
	program := bytecode.Program{Entry: -1, Records: []ir.Record{{Fields: []ir.Kind{ir.Int}}, {Fields: []ir.Kind{ir.Int}}}, Functions: []bytecode.Function{
		{Result: kind, Code: []bytecode.Instruction{{Op: bytecode.Const, Kind: ir.Int, Int: 42}, {Op: bytecode.MakeRecord, Kind: kind}, {Op: bytecode.Return}}},
		{Params: []ir.Kind{kind}, Locals: []ir.Kind{kind}, Result: ir.Int, Code: []bytecode.Instruction{{Op: bytecode.Load}, {Op: bytecode.GetField, Kind: kind}, {Op: bytecode.Return}}},
		{Params: []ir.Kind{kind + 1}, Locals: []ir.Kind{kind + 1}, Result: ir.Void, Code: []bytecode.Instruction{{Op: bytecode.Return}}},
		{Params: []ir.Kind{ir.Int}, Locals: []ir.Kind{ir.Int}, Result: ir.Void, Code: []bytecode.Instruction{{Op: bytecode.Return}}},
	}}
	makeRuntime := func() *Runtime {
		t.Helper()
		runtime, err := New(program, nil, Limits{Instructions: 20, Frames: 1})
		if err != nil {
			t.Fatal(err)
		}
		return runtime
	}
	first, other := makeRuntime(), makeRuntime()
	program.Records[0].Fields[0] = ir.Bool
	record, err := first.Invoke(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	copy := record
	copy.Int = 99
	got, err := first.Invoke(1, []Value{copy})
	if err != nil || got != (Value{Kind: ir.Int, Int: 42}) {
		t.Fatalf("projection returned %v, %v", got, err)
	}
	_, err = other.Invoke(1, []Value{record})
	requireError(t, err, InvalidInvocation, 0, 0, token.Position{})
	_, err = first.Invoke(1, []Value{{Kind: kind}})
	requireError(t, err, InvalidInvocation, 0, 0, token.Position{})
	copy.Kind = kind + 1
	_, err = first.Invoke(2, []Value{copy})
	requireError(t, err, InvalidInvocation, 0, 0, token.Position{})
	copy.Kind = ir.Int
	_, err = first.Invoke(3, []Value{copy})
	requireError(t, err, InvalidInvocation, 0, 0, token.Position{})
}

func TestDispatchBudgetIsGlobalAcrossFrames(t *testing.T) {
	callSite := token.Position{File: "recursion.splash", Line: 3, Column: 5}
	program := bytecode.Program{Functions: []bytecode.Function{
		{Result: ir.Void, Code: []bytecode.Instruction{{Op: bytecode.Call, Source: callSite}, {Op: bytecode.Return}}},
	}}
	for _, test := range []struct {
		limits Limits
		code   ErrorCode
		count  uint64
		depth  int
	}{
		{Limits{Instructions: 2, Frames: 8}, InstructionLimit, 2, 3},
		{Limits{Instructions: 8, Frames: 2}, FrameLimit, 2, 2},
	} {
		runtime, err := New(program, nil, test.limits)
		if err != nil {
			t.Fatal(err)
		}
		requireError(t, runtime.RunMain(), test.code, test.count, test.depth, callSite)
	}
}

func TestLoopConsumesBudgetAtRepeatedInstruction(t *testing.T) {
	pos := token.Position{File: "loop.splash", Line: 1, Column: 1}
	program := bytecode.Program{Functions: []bytecode.Function{
		{Result: ir.Void, Code: []bytecode.Instruction{{Op: bytecode.Jump, Source: pos}}},
	}}
	runtime, err := New(program, nil, Limits{Instructions: 4, Frames: 1})
	if err != nil {
		t.Fatal(err)
	}
	requireError(t, runtime.RunMain(), InstructionLimit, 4, 1, pos)
}

func TestCheckedArithmetic(t *testing.T) {
	pos := token.Position{File: "math.splash", Line: 1, Column: 3}
	for _, tt := range []struct {
		name        string
		op          bytecode.Op
		left, right int64
		want        int64
		code        ErrorCode
	}{
		{"positive add overflow", bytecode.Add, math.MaxInt64, 1, 0, IntegerOverflow},
		{"negative add overflow", bytecode.Add, math.MinInt64, -1, 0, IntegerOverflow},
		{"positive sub overflow", bytecode.Sub, math.MaxInt64, -1, 0, IntegerOverflow},
		{"negative sub overflow", bytecode.Sub, math.MinInt64, 1, 0, IntegerOverflow},
		{"multiply overflow", bytecode.Mul, math.MaxInt64, 2, 0, IntegerOverflow},
		{"minimum multiply overflow", bytecode.Mul, -1, math.MinInt64, 0, IntegerOverflow},
		{"minimum multiply by one", bytecode.Mul, math.MinInt64, 1, math.MinInt64, ""},
		{"divide overflow", bytecode.Div, math.MinInt64, -1, 0, IntegerOverflow},
		{"divide by zero", bytecode.Div, 1, 0, 0, DivisionByZero},
		{"remainder by zero", bytecode.Mod, 1, 0, 0, DivisionByZero},
		{"division truncates toward zero", bytecode.Div, -5, 2, -2, ""},
		{"negative remainder", bytecode.Mod, -5, 2, -1, ""},
		{"minimum modulo", bytecode.Mod, math.MinInt64, -1, 0, ""},
		{"negate overflow", bytecode.Neg, math.MinInt64, 0, 0, IntegerOverflow},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code := []bytecode.Instruction{{Op: bytecode.Const, Kind: ir.Int, Int: tt.left}}
			if tt.op != bytecode.Neg {
				code = append(code, bytecode.Instruction{Op: bytecode.Const, Kind: ir.Int, Int: tt.right})
			}
			code = append(code, bytecode.Instruction{Op: tt.op, Source: pos}, bytecode.Instruction{Op: bytecode.Return})
			runtime, err := New(bytecode.Program{Entry: -1, Functions: []bytecode.Function{{Result: ir.Int, Code: code}}}, nil, Limits{Instructions: 4, Frames: 1})
			if err != nil {
				t.Fatal(err)
			}
			got, err := runtime.Invoke(0, nil)
			if tt.code != "" {
				requireError(t, err, tt.code, uint64(len(code)-1), 1, pos)
			} else if err != nil || got != (Value{Kind: ir.Int, Int: tt.want}) {
				t.Fatalf("got %v, %v, want Int(%d)", got, err, tt.want)
			}
		})
	}
}

func TestNewValidatesAndOwnsBytecode(t *testing.T) {
	var out bytes.Buffer
	program := bytecode.Program{Functions: []bytecode.Function{{Result: ir.Void, Code: []bytecode.Instruction{
		{Op: bytecode.Const, Kind: ir.String, Text: "original"}, {Op: bytecode.Print}, {Op: bytecode.Return},
	}}}}
	runtime, err := New(program, &out, Limits{Instructions: 3, Frames: 1})
	if err != nil {
		t.Fatal(err)
	}
	program.Functions[0].Code[0].Text = "replaced"
	program.Functions[0].Code[2].Op = 255
	if _, err := New(program, &out, Limits{}); err == nil || out.Len() != 0 {
		t.Fatalf("malformed program error = %v, output = %q", err, out.String())
	}
	if err := runtime.RunMain(); err != nil || out.String() != "original\n" {
		t.Fatalf("validated runtime returned %v and wrote %q", err, out.String())
	}
}

func requireError(t *testing.T, err error, code ErrorCode, count uint64, depth int, pos token.Position) {
	t.Helper()
	var vmErr *Error
	if !errors.As(err, &vmErr) || vmErr.Code != code || vmErr.InstructionCount != count || vmErr.FrameDepth != depth || vmErr.Source != pos {
		t.Fatalf("want %s at %s with count=%d depth=%d, got %#v", code, pos, count, depth, err)
	}
}

func TestLoweringRetainsLiteralAndPrintProvenance(t *testing.T) {
	literal := token.Position{File: "hello.splash", Line: 4, Column: 9}
	printSite := token.Position{File: "hello.splash", Line: 3, Column: 5}
	program, err := bytecode.Lower(ir.Program{Functions: []ir.Function{{Result: ir.Void, Blocks: []ir.Block{{
		Ops: []ir.Instruction{
			{Op: ir.Const, Type: ir.String, Text: "hello", Source: literal},
			{Op: ir.Print, Type: ir.Void, Args: []int{0}, Source: printSite},
		},
		End: ir.Terminator{Op: ir.Return, Value: -1},
	}}}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(program, nil, Limits{Frames: 1})
	if err != nil {
		t.Fatal(err)
	}
	requireError(t, runtime.RunMain(), InstructionLimit, 0, 1, literal)

	runtime, err = New(program, nil, Limits{Instructions: 20, Frames: 1})
	if err != nil {
		t.Fatal(err)
	}
	var outputError *Error
	if err := runtime.RunMain(); !errors.As(err, &outputError) || outputError.Code != OutputError || outputError.Source != printSite {
		t.Fatalf("want output error at %s, got %v", printSite, err)
	}
}
