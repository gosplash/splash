package bytecode

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"gosplash.dev/splash/internal/ir"
	"gosplash.dev/splash/internal/token"
)

func TestValidateRejectsMalformedFlow(t *testing.T) {
	pos := token.Position{File: "test.splash", Line: 5, Column: 3}
	intConst := Instruction{Op: Const, Kind: ir.Int, Int: 1}
	boolConst := Instruction{Op: Const, Kind: ir.Bool}
	tests := []struct {
		name   string
		locals []ir.Kind
		code   []Instruction
	}{
		{"branch outside code", nil, []Instruction{{Op: Jump, Index: 1, Source: pos}}},
		{"mismatched stack on join", nil, []Instruction{
			boolConst, {Op: JumpIfFalse, Index: 4}, intConst, {Op: Jump, Index: 5},
			{Op: Const, Kind: ir.Bool}, {Op: Print, Source: pos}, {Op: Return},
		}},
		{"mismatched stack height on join", nil, []Instruction{
			boolConst, {Op: JumpIfFalse, Index: 4}, intConst, {Op: Jump, Index: 4}, {Op: Return, Source: pos},
		}},
		{"mismatched stack on loop back edge", nil, []Instruction{
			{Op: Const, Kind: ir.Int, Source: pos}, {Op: Jump},
		}},
		{"local assigned on only one branch", []ir.Kind{ir.Int}, []Instruction{
			boolConst, {Op: JumpIfFalse, Index: 5}, intConst, {Op: Store}, {Op: Jump, Index: 5},
			{Op: Load, Source: pos}, {Op: Print}, {Op: Return},
		}},
		{"call argument has wrong type", nil, []Instruction{boolConst, {Op: Call, Index: 1, Source: pos}, {Op: Return}}},
		{"call argument missing", nil, []Instruction{{Op: Call, Index: 1, Source: pos}, {Op: Return}}},
		{"cannot fall through function", nil, []Instruction{intConst, {Op: Print, Source: pos}}},
		{"unreachable instruction has invalid opcode", nil, []Instruction{{Op: Return}, {Op: 255, Source: pos}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := Program{Functions: []Function{
				{Result: ir.Void, Locals: tt.locals, Code: tt.code},
				{Params: []ir.Kind{ir.Int}, Result: ir.Void, Locals: []ir.Kind{ir.Int}, Code: []Instruction{{Op: Return}}},
			}}
			var validation *ValidationError
			err := Validate(program)
			if !errors.As(err, &validation) || validation.Source != pos {
				t.Fatalf("want validation error at %s, got %v", pos, err)
			}
		})
	}
}

func TestValidateRevisitsLoopHeaderAfterJoin(t *testing.T) {
	load := token.Position{File: "loop.splash", Line: 2, Column: 5}
	code := []Instruction{
		{Op: Const, Kind: ir.Bool}, {Op: JumpIfFalse, Index: 5},
		{Op: Const, Kind: ir.Int}, {Op: Store}, {Op: Jump, Index: 8},
		{Op: Jump, Index: 6}, {Op: Jump, Index: 7}, {Op: Jump, Index: 8},
		{Op: Load, Source: load}, {Op: Print}, {Op: Jump, Index: 8},
	}
	program := Program{Entry: -1, Functions: []Function{{Result: ir.Void, Locals: []ir.Kind{ir.Int}, Code: code}}}
	var invalid *ValidationError
	if err := Validate(program); !errors.As(err, &invalid) || invalid.Source != load {
		t.Fatalf("want uninitialized load at %s, got %v", load, err)
	}
	program.Functions[0].Params = []ir.Kind{ir.Int}
	if err := Validate(program); err != nil {
		t.Fatalf("parameter remains initialized around the loop: %v", err)
	}
}

func TestValidateRequiresUTF8InStringConstants(t *testing.T) {
	pos := token.Position{File: "strings.splash", Line: 2, Column: 5}
	code := []Instruction{{Op: Return}, {Op: Const, Kind: ir.String, Text: "水🌊", Source: pos}}
	program := Program{Functions: []Function{{Result: ir.Void, Code: code}}}
	if err := Validate(program); err != nil {
		t.Fatal(err)
	}
	code[1].Text = "\xff"
	var invalid *ValidationError
	if err := Validate(program); !errors.As(err, &invalid) || invalid.Source != pos {
		t.Fatalf("want invalid string constant at %s, got %v", pos, err)
	}
}

func TestValidateChecksRecordOperands(t *testing.T) {
	first, second := ir.RecordBase, ir.RecordBase+1
	pos := token.Position{File: "records.splash", Line: 5, Column: 5}
	for _, tc := range []struct {
		name   string
		prefix []Instruction
		op     Instruction
	}{
		{"missing field", nil, Instruction{Op: MakeRecord, Kind: first}},
		{"wrong field type", []Instruction{{Op: Const, Kind: ir.Bool}}, Instruction{Op: MakeRecord, Kind: first}},
		{"wrong nominal record", []Instruction{{Op: Const, Kind: ir.Int}, {Op: MakeRecord, Kind: second}}, Instruction{Op: GetField, Kind: first}},
		{"unreachable invalid field slot", []Instruction{{Op: Return}}, Instruction{Op: GetField, Kind: first, Index: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op := tc.op
			op.Source = pos
			program := Program{Records: []ir.Record{{Fields: []ir.Kind{ir.Int}}, {Fields: []ir.Kind{ir.Int}}}, Functions: []Function{
				{Result: ir.Void, Code: append(tc.prefix, op, Instruction{Op: Return})},
			}}
			var invalid *ValidationError
			if err := Validate(program); !errors.As(err, &invalid) || invalid.Source != pos {
				t.Fatalf("want record error at %s, got %v", pos, err)
			}
		})
	}
}

func BenchmarkValidateStraightLine(b *testing.B) {
	for _, size := range []struct{ operations, locals int }{
		{32, 32}, {128, 128}, {512, 512}, {1024, 1024}, {1024, 32}, {32, 1024},
	} {
		b.Run(fmt.Sprintf("instructions=%d/locals=%d", 4*size.operations+1, size.locals), func(b *testing.B) {
			locals := slices.Repeat([]ir.Kind{ir.Int}, size.locals)
			code := make([]Instruction, 0, 4*size.operations+1)
			for i := 0; i < size.operations; i++ {
				slot := i % len(locals)
				code = append(code, Instruction{Op: Const, Kind: ir.Int}, Instruction{Op: Store, Index: slot},
					Instruction{Op: Load, Index: slot}, Instruction{Op: Print})
			}
			code = append(code, Instruction{Op: Return})
			program := Program{Functions: []Function{{Result: ir.Void, Locals: locals, Code: code}}}
			b.ReportAllocs()
			for b.Loop() {
				if err := Validate(program); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestValidateAcceptsLocalAssignedAcrossBothBranches(t *testing.T) {
	program := Program{Functions: []Function{{Result: ir.Void, Locals: []ir.Kind{ir.Int}, Code: []Instruction{
		{Op: Const, Kind: ir.Bool}, {Op: JumpIfFalse, Index: 5},
		{Op: Const, Kind: ir.Int, Int: 1}, {Op: Store}, {Op: Jump, Index: 7},
		{Op: Const, Kind: ir.Int, Int: 2}, {Op: Store},
		{Op: Load}, {Op: Print}, {Op: Return},
	}}}}
	if err := Validate(program); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsInvalidApplicationEntrySignature(t *testing.T) {
	pos := token.Position{File: "application.splash", Line: 4, Column: 1}
	for _, fn := range []Function{
		{Params: []ir.Kind{ir.Int}, Locals: []ir.Kind{ir.Int}, Result: ir.Void, Source: pos, Code: []Instruction{{Op: Return}}},
		{Result: ir.Int, Source: pos, Code: []Instruction{{Op: Const, Kind: ir.Int}, {Op: Return}}},
	} {
		var invalid *ValidationError
		err := Validate(Program{Entry: 0, Functions: []Function{fn}})
		if !errors.As(err, &invalid) || invalid.Source != pos {
			t.Fatalf("want invalid main signature at %s, got %v", pos, err)
		}
		if err := Validate(Program{Entry: -1, Functions: []Function{fn}}); err != nil {
			t.Fatalf("valid direct-invocation program: %v", err)
		}
	}
}
