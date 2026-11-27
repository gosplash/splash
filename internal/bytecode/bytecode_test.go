package bytecode

import (
	"errors"
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
