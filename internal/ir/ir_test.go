package ir

import (
	"errors"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/token"
)

func TestValidateRequiresUTF8InStringConstants(t *testing.T) {
	pos := token.Position{File: "strings.splash", Line: 2, Column: 5}
	ops := []Instruction{{Op: Const, Type: String, Text: "水🌊", Source: pos}}
	program := Program{Functions: []Function{{Result: Void, Blocks: []Block{{Ops: ops, End: Terminator{Op: Return, Value: -1}}}}}}
	if err := Validate(program); err != nil {
		t.Fatal(err)
	}
	ops[0].Text = "\xff"
	var invalid *ValidationError
	if err := Validate(program); !errors.As(err, &invalid) || invalid.Source != pos {
		t.Fatalf("want invalid string constant at %s, got %v", pos, err)
	}
}

func TestValidateTypedRecursiveCalls(t *testing.T) {
	program := Program{Functions: []Function{
		{
			Result: Void,
			Blocks: []Block{{
				Ops: []Instruction{
					{Op: Const, Type: Int, Dest: 0, Int: 5},
					{Op: Call, Type: Int, Dest: 1, Index: 1, Args: []int{0}},
					{Op: Print, Type: Void, Args: []int{1}},
				},
				End: Terminator{Op: Return, Value: -1},
			}},
		},
		{
			Params: []Kind{Int},
			Result: Int,
			Locals: []Kind{Int},
			Blocks: []Block{
				{
					Ops: []Instruction{
						{Op: Load, Type: Int, Dest: 0},
						{Op: Const, Type: Int, Dest: 1},
						{Op: Eq, Type: Bool, Dest: 2, Args: []int{0, 1}},
					},
					End: Terminator{Op: Branch, Value: 2, Target: 1, Else: 2},
				},
				{
					Ops: []Instruction{{Op: Const, Type: Int, Dest: 3}},
					End: Terminator{Op: Return, Value: 3},
				},
				{
					Ops: []Instruction{
						{Op: Load, Type: Int, Dest: 4},
						{Op: Const, Type: Int, Dest: 5, Int: 1},
						{Op: Sub, Type: Int, Dest: 6, Args: []int{4, 5}},
						{Op: Call, Type: Int, Dest: 7, Index: 1, Args: []int{6}},
					},
					End: Terminator{Op: Return, Value: 7},
				},
			},
		},
	}}
	if err := Validate(program); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRequiresInitializationOnEveryIncomingPath(t *testing.T) {
	program := Program{Functions: []Function{{
		Result: Void,
		Locals: []Kind{Int},
		Blocks: []Block{
			{
				Ops: []Instruction{{Op: Const, Type: Bool, Dest: 0}},
				End: Terminator{Op: Branch, Value: 0, Target: 1, Else: 2},
			},
			{
				Ops: []Instruction{
					{Op: Const, Type: Int, Dest: 1, Int: 1},
					{Op: Store, Type: Void, Index: 0, Args: []int{1}},
				},
				End: Terminator{Op: Jump, Target: 3},
			},
			{
				Ops: []Instruction{
					{Op: Const, Type: Int, Dest: 2, Int: 2},
					{Op: Store, Type: Void, Index: 0, Args: []int{2}},
				},
				End: Terminator{Op: Jump, Target: 3},
			},
			{
				Ops: []Instruction{{Op: Load, Type: Int, Dest: 3, Index: 0}},
				End: Terminator{Op: Return, Value: -1},
			},
		},
	}}}
	if err := Validate(program); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []int{1, 2} {
		block := &program.Functions[0].Blocks[missing]
		stores := block.Ops
		block.Ops = nil
		if err := Validate(program); err == nil || !strings.Contains(err.Error(), "uninitialized local") {
			t.Fatalf("missing initialization in block %d was accepted: %v", missing, err)
		}
		block.Ops = stores
	}
}

func TestValidateRejectsMalformedIR(t *testing.T) {
	position := token.Position{File: "test.splash", Line: 3, Column: 4}
	for _, test := range []struct {
		name, want string
		mutate     func(*Program)
	}{
		{"invalid entry", "entry", func(p *Program) { p.Entry = -1 }},
		{"parameter slot", "parameter", func(p *Program) { p.Functions[1].Locals = []Kind{Bool} }},
		{"unknown op", "unknown operation", func(p *Program) { p.Functions[0].Blocks[0].Ops[0].Op = 0 }},
		{"wrong call argument", "operand", func(p *Program) { p.Functions[0].Blocks[0].Ops[0].Type = Bool }},
		{"invalid callee", "call target", func(p *Program) { p.Functions[0].Blocks[0].Ops[1].Index = 2 }},
		{"invalid branch target", "branch target", func(p *Program) {
			p.Functions[0].Blocks[0].End = Terminator{Op: Jump, Target: 1}
		}},
		{"nonboolean branch", "Bool", func(p *Program) {
			p.Functions[0].Blocks[0].End = Terminator{Op: Branch, Value: 0, Target: 0, Else: 0}
		}},
		{"duplicate result", "duplicate", func(p *Program) {
			p.Functions[0].Blocks[0].Ops = append(p.Functions[0].Blocks[0].Ops, Instruction{Op: Const, Type: Int, Dest: 0})
		}},
		{"missing return operand", "return", func(p *Program) {
			p.Functions[1].Blocks[0].End.Value = -1
		}},
		{"across-block operand", "undefined temporary", func(p *Program) {
			p.Functions[0].Blocks[0].End = Terminator{Op: Jump, Target: 1}
			p.Functions[0].Blocks = append(p.Functions[0].Blocks,
				Block{Ops: []Instruction{{Op: Print, Type: Void, Args: []int{0}}}, End: Terminator{Op: Return, Value: -1}})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			program := Program{Functions: []Function{
				{
					Result: Void,
					Blocks: []Block{{
						Ops: []Instruction{
							{Op: Const, Type: Int, Dest: 0, Source: position},
							{Op: Call, Type: Int, Dest: 1, Index: 1, Args: []int{0}},
						},
						End: Terminator{Op: Return, Value: -1},
					}},
				},
				{
					Result: Int,
					Params: []Kind{Int},
					Locals: []Kind{Int},
					Blocks: []Block{{
						Ops: []Instruction{{Op: Load, Type: Int, Dest: 0}},
						End: Terminator{Op: Return, Value: 0},
					}},
				},
			}}
			test.mutate(&program)
			var diagnostic *ValidationError
			err := Validate(program)
			if !errors.As(err, &diagnostic) || !strings.Contains(diagnostic.Detail, test.want) {
				t.Fatalf("want validation error containing %q, got %v", test.want, err)
			}
			if test.name == "unknown op" && diagnostic.Source != position {
				t.Fatalf("want %v, got %v", position, diagnostic.Source)
			}
		})
	}
}
