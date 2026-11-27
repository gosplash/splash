// Package ir defines and validates Splash's backend-independent typed operations.
package ir

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"gosplash.dev/splash/internal/token"
)

type Kind uint8

const (
	Void Kind = iota + 1
	String
	Int
	Bool
)

func (kind Kind) String() string {
	switch kind {
	case Void:
		return "Void"
	case String:
		return "String"
	case Int:
		return "Int"
	case Bool:
		return "Bool"
	default:
		return fmt.Sprintf("invalid kind %d", kind)
	}
}

type Program struct {
	Functions []Function
	Entry     int
}

type Function struct {
	Params []Kind
	Result Kind
	Locals []Kind // Parameters occupy the first slots.
	Blocks []Block
	Source token.Position
}

type Block struct {
	Ops []Instruction
	End Terminator
}

type Op uint8

const (
	Const Op = iota + 1
	Load
	Store
	Add
	Sub
	Mul
	Div
	Mod
	Eq
	Ne
	Lt
	Le
	Gt
	Ge
	Neg
	Not
	Call
	Print
)

// Instruction defines a fresh temporary in Dest when Type is non-void.
// Args reads temporaries in this block in evaluation order. Index selects a
// local slot for loads and stores, or a function for calls.
type Instruction struct {
	Op     Op
	Type   Kind
	Dest   int
	Args   []int
	Index  int
	Int    int64
	Text   string
	Bool   bool
	Source token.Position
}

type TermOp uint8

const (
	Jump TermOp = iota + 1
	Branch
	Return
)

type Terminator struct {
	Op     TermOp
	Value  int // Condition for Branch, result for Return, or -1 for a Void return.
	Target int // Destination for Jump, true destination for Branch.
	Else   int // False destination for Branch.
	Source token.Position
}

type ValidationError struct {
	Source token.Position
	Detail string
}

func (e *ValidationError) Error() string {
	const code = "SPLASH_VM_INVALID_IR"
	if e.Source.File == "" {
		return fmt.Sprintf("%s: %s", code, e.Detail)
	}
	return fmt.Sprintf("%s: %s: %s", e.Source, code, e.Detail)
}

func invalid(source token.Position, format string, args ...any) error {
	return &ValidationError{Source: source, Detail: fmt.Sprintf(format, args...)}
}

// Validate checks structure and types everywhere, and definite local
// initialization on every reachable control-flow path.
func Validate(program Program) error {
	if program.Entry < 0 || program.Entry >= len(program.Functions) {
		return invalid(token.Position{}, "invalid entry function %d", program.Entry)
	}
	entry := program.Functions[program.Entry]
	if len(entry.Params) != 0 || entry.Result != Void {
		return invalid(entry.Source, "entry function must take no parameters and return Void")
	}
	for _, function := range program.Functions {
		if function.Result != Void && !isValue(function.Result) {
			return invalid(function.Source, "invalid function result type %s", function.Result)
		}
		if len(function.Params) > len(function.Locals) {
			return invalid(function.Source, "missing parameter slots")
		}
		for slot, kind := range function.Locals {
			if slot < len(function.Params) && kind != function.Params[slot] {
				return invalid(function.Source, "parameter %d does not match its local slot", slot)
			}
			if !isValue(kind) {
				return invalid(function.Source, "invalid local type %s in slot %d", kind, slot)
			}
		}
		if len(function.Blocks) == 0 {
			return invalid(function.Source, "function has no entry block")
		}
	}
	for _, function := range program.Functions {
		if err := validateBlocks(program, function); err != nil {
			return err
		}
		if err := validateInitialization(function); err != nil {
			return err
		}
	}
	return nil
}

func validateBlocks(program Program, function Function) error {
	definitions := make(map[int]bool)
	for _, block := range function.Blocks {
		temporaries := make(map[int]Kind)
		for _, inst := range block.Ops {
			operands := make([]Kind, 0, len(inst.Args))
			for _, arg := range inst.Args {
				kind, ok := temporaries[arg]
				if !ok {
					return invalid(inst.Source, "undefined temporary %d in block", arg)
				}
				operands = append(operands, kind)
			}
			if err := validateInstruction(program, function, inst, operands); err != nil {
				return err
			}
			if inst.Type != Void {
				if inst.Dest < 0 || definitions[inst.Dest] {
					return invalid(inst.Source, "invalid or duplicate temporary definition %d", inst.Dest)
				}
				definitions[inst.Dest] = true
				temporaries[inst.Dest] = inst.Type
			}
		}
		if err := validateTerminator(function, block.End, temporaries); err != nil {
			return err
		}
	}
	return nil
}

func validateInstruction(program Program, function Function, inst Instruction, operands []Kind) error {
	expect := func(result Kind, args ...Kind) error {
		if inst.Type != result || !slices.Equal(operands, args) {
			return invalid(inst.Source, "invalid result or operand types for operation %d", inst.Op)
		}
		return nil
	}
	switch inst.Op {
	case Const:
		if !isValue(inst.Type) || len(operands) != 0 {
			return invalid(inst.Source, "constant requires a value type and no operands")
		}
		if inst.Type == String && !utf8.ValidString(inst.Text) {
			return invalid(inst.Source, "string constant must be valid UTF-8")
		}
		return nil
	case Load, Store:
		if inst.Index < 0 || inst.Index >= len(function.Locals) {
			return invalid(inst.Source, "local slot %d out of range", inst.Index)
		}
		kind := function.Locals[inst.Index]
		if inst.Op == Load {
			return expect(kind)
		}
		return expect(Void, kind)
	case Add, Sub, Mul, Div, Mod:
		return expect(Int, Int, Int)
	case Lt, Le, Gt, Ge:
		return expect(Bool, Int, Int)
	case Eq, Ne:
		if len(operands) > 0 && operands[0] == Bool {
			return expect(Bool, Bool, Bool)
		}
		return expect(Bool, Int, Int)
	case Neg:
		return expect(Int, Int)
	case Not:
		return expect(Bool, Bool)
	case Call:
		if inst.Index < 0 || inst.Index >= len(program.Functions) {
			return invalid(inst.Source, "call target %d out of range", inst.Index)
		}
		callee := program.Functions[inst.Index]
		return expect(callee.Result, callee.Params...)
	case Print:
		if inst.Type != Void || len(operands) != 1 || !isValue(operands[0]) {
			return invalid(inst.Source, "print requires one value operand and a Void result")
		}
		return nil
	default:
		return invalid(inst.Source, "unknown operation %d", inst.Op)
	}
}

func isValue(kind Kind) bool {
	return kind == Int || kind == Bool || kind == String
}

func validateTerminator(function Function, end Terminator, temporaries map[int]Kind) error {
	switch end.Op {
	case Jump:
		if end.Target < 0 || end.Target >= len(function.Blocks) {
			return invalid(end.Source, "branch target %d out of range", end.Target)
		}
	case Branch:
		if temporaries[end.Value] != Bool {
			return invalid(end.Source, "branch requires a Bool condition defined in the block")
		}
		if end.Target < 0 || end.Target >= len(function.Blocks) || end.Else < 0 || end.Else >= len(function.Blocks) {
			return invalid(end.Source, "branch target out of range")
		}
	case Return:
		if function.Result == Void {
			if end.Value != -1 {
				return invalid(end.Source, "Void return cannot return a value")
			}
		} else if temporaries[end.Value] != function.Result {
			return invalid(end.Source, "return requires a %s value defined in the block", function.Result)
		}
	default:
		return invalid(end.Source, "missing or invalid block terminator")
	}
	return nil
}

func validateInitialization(function Function) error {
	inputs := make([][]bool, len(function.Blocks))
	inputs[0] = make([]bool, len(function.Locals))
	for slot := range function.Params {
		inputs[0][slot] = true
	}
	queued := make([]bool, len(function.Blocks))
	queued[0] = true
	work := []int{0}

	// Intersect predecessor states. Once a slot is uninitialized at a block
	// entry, a later predecessor cannot turn it into an initialized slot.
	for len(work) > 0 {
		index := work[len(work)-1]
		work = work[:len(work)-1]
		queued[index] = false
		block := function.Blocks[index]
		state := slices.Clone(inputs[index])
		for _, inst := range block.Ops {
			switch inst.Op {
			case Store:
				state[inst.Index] = true
			case Load:
				if !state[inst.Index] {
					return invalid(inst.Source, "load from uninitialized local %d", inst.Index)
				}
			}
		}
		if block.End.Op == Return {
			continue
		}
		targets := []int{block.End.Target}
		if block.End.Op == Branch {
			targets = append(targets, block.End.Else)
		}
		for _, target := range targets {
			input := inputs[target]
			changed := input == nil
			if input == nil {
				inputs[target] = slices.Clone(state)
			} else {
				for slot := range input {
					if input[slot] && !state[slot] {
						input[slot] = false
						changed = true
					}
				}
			}
			if changed && !queued[target] {
				work = append(work, target)
				queued[target] = true
			}
		}
	}
	return nil
}
