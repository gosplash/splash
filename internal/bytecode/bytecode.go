// Package bytecode lowers validated typed IR and independently checks the
// resulting instruction stream before it can be executed.
package bytecode

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"gosplash.dev/splash/internal/ir"
	"gosplash.dev/splash/internal/token"
)

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
	MakeRecord
	GetField
	Jump
	JumpIfFalse
	Return
)

// Instruction uses Index for a local, function, branch target, or field slot.
// Const specifies a primitive kind. Record operations specify a record kind.
type Instruction struct {
	Op     Op
	Index  int
	Kind   ir.Kind
	Int    int64
	Bool   bool
	Text   string
	Source token.Position
}

type Function struct {
	Params []ir.Kind
	Result ir.Kind
	Locals []ir.Kind // Parameter slots come first.
	Code   []Instruction
	Source token.Position
}

type Program struct {
	Records   []ir.Record
	Functions []Function
	Entry     int // -1 for a program only used through direct invocation.
}

type ValidationError struct {
	Source token.Position
	Detail string
}

func (e *ValidationError) Error() string {
	if e.Source.File == "" {
		return e.Detail
	}
	return fmt.Sprintf("%s: %s", e.Source, e.Detail)
}

// Lower validates IR even when the module came from the source compiler.
func Lower(input ir.Program) (Program, error) {
	if err := ir.Validate(input); err != nil {
		return Program{}, err
	}
	result := Program{Entry: input.Entry, Records: ir.CloneRecords(input.Records), Functions: make([]Function, len(input.Functions))}
	for index, fn := range input.Functions {
		result.Functions[index] = lowerFunction(fn)
	}
	return result, nil
}

func lowerFunction(fn ir.Function) Function {
	out := Function{Params: slices.Clone(fn.Params), Result: fn.Result, Locals: slices.Clone(fn.Locals), Source: fn.Source}
	temps := make(map[int]int)
	blockStarts := make([]int, len(fn.Blocks))
	var branches []int
	for index, block := range fn.Blocks {
		blockStarts[index] = len(out.Code)
		for _, operation := range block.Ops {
			for _, argument := range operation.Args {
				out.Code = append(out.Code, Instruction{Op: Load, Index: temps[argument], Source: operation.Source})
			}
			instruction := Instruction{Op: instructionOp(operation.Op), Source: operation.Source}
			switch operation.Op {
			case ir.Load, ir.Store, ir.Call:
				instruction.Index = operation.Index
			case ir.Const:
				instruction.Kind, instruction.Int = operation.Type, operation.Int
				instruction.Bool, instruction.Text = operation.Bool, operation.Text
			case ir.MakeRecord:
				instruction.Kind = operation.Type
			case ir.GetField:
				instruction.Kind = out.Locals[temps[operation.Args[0]]]
				instruction.Index = operation.Index
			}
			out.Code = append(out.Code, instruction)
			if operation.Type != ir.Void {
				slot := len(out.Locals)
				temps[operation.Dest] = slot
				out.Locals = append(out.Locals, operation.Type)
				out.Code = append(out.Code, Instruction{Op: Store, Index: slot, Source: operation.Source})
			}
		}
		end := block.End
		switch end.Op {
		case ir.Jump:
			branches = append(branches, len(out.Code))
			out.Code = append(out.Code, Instruction{Op: Jump, Index: end.Target, Source: end.Source})
		case ir.Branch:
			out.Code = append(out.Code, Instruction{Op: Load, Index: temps[end.Value], Source: end.Source})
			branches = append(branches, len(out.Code), len(out.Code)+1)
			out.Code = append(out.Code,
				Instruction{Op: JumpIfFalse, Index: end.Else, Source: end.Source},
				Instruction{Op: Jump, Index: end.Target, Source: end.Source},
			)
		case ir.Return:
			if end.Value >= 0 {
				out.Code = append(out.Code, Instruction{Op: Load, Index: temps[end.Value], Source: end.Source})
			}
			out.Code = append(out.Code, Instruction{Op: Return, Source: end.Source})
		}
	}
	for _, pc := range branches {
		out.Code[pc].Index = blockStarts[out.Code[pc].Index]
	}
	return out
}

func instructionOp(op ir.Op) Op {
	return [...]Op{
		ir.Const: Const, ir.Load: Load, ir.Store: Store, ir.Add: Add, ir.Sub: Sub, ir.Mul: Mul,
		ir.Div: Div, ir.Mod: Mod, ir.Eq: Eq, ir.Ne: Ne, ir.Lt: Lt, ir.Le: Le, ir.Gt: Gt,
		ir.Ge: Ge, ir.Neg: Neg, ir.Not: Not, ir.Call: Call, ir.Print: Print,
		ir.MakeRecord: MakeRecord, ir.GetField: GetField,
	}[op]
}

// Validate checks all instruction encodings and runs typed stack and local
// initialization analysis over every reachable control-flow edge.
func Validate(program Program) error {
	if program.Entry < -1 || program.Entry >= len(program.Functions) || len(program.Functions) == 0 {
		return &ValidationError{Detail: "invalid program entry or missing function"}
	}
	if program.Entry >= 0 {
		entry := program.Functions[program.Entry]
		if len(entry.Params) != 0 || entry.Result != ir.Void {
			return &ValidationError{Source: entry.Source, Detail: "entry function must take no parameters and return Void"}
		}
	}
	for index, record := range program.Records {
		for field, kind := range record.Fields {
			if !ir.IsPrimitiveValue(kind) {
				return &ValidationError{Detail: fmt.Sprintf("record %d field %d requires a primitive value type", index, field)}
			}
		}
	}
	for _, fn := range program.Functions {
		if err := validateFunctionEncoding(fn, program); err != nil {
			return err
		}
	}
	for _, fn := range program.Functions {
		if err := validateFlow(fn, program); err != nil {
			return err
		}
	}
	return nil
}

func validateFunctionEncoding(fn Function, program Program) error {
	if len(fn.Code) == 0 || len(fn.Params) > len(fn.Locals) ||
		!slices.Equal(fn.Params, fn.Locals[:len(fn.Params)]) ||
		fn.Result != ir.Void && !ir.IsValue(program.Records, fn.Result) {
		return &ValidationError{Source: fn.Source, Detail: "invalid function signature or missing code"}
	}
	for _, local := range fn.Locals {
		if !ir.IsValue(program.Records, local) {
			return &ValidationError{Source: fn.Source, Detail: "invalid local type"}
		}
	}
	for _, instruction := range fn.Code {
		var limit int
		switch instruction.Op {
		case Const:
			if !ir.IsPrimitiveValue(instruction.Kind) || instruction.Index != 0 {
				return &ValidationError{Source: instruction.Source, Detail: "invalid constant operands"}
			}
			if instruction.Kind == ir.String && !utf8.ValidString(instruction.Text) {
				return &ValidationError{Source: instruction.Source, Detail: "string constant must be valid UTF-8"}
			}
			continue
		case MakeRecord, GetField:
			record, ok := ir.RecordFor(program.Records, instruction.Kind)
			if !ok || instruction.Index < 0 ||
				instruction.Op == MakeRecord && instruction.Index != 0 ||
				instruction.Op == GetField && instruction.Index >= len(record.Fields) {
				return &ValidationError{Source: instruction.Source, Detail: "invalid record instruction operands"}
			}
			continue
		case Load, Store:
			limit = len(fn.Locals)
		case Call:
			limit = len(program.Functions)
		case Jump, JumpIfFalse:
			limit = len(fn.Code)
		case Add, Sub, Mul, Div, Mod, Eq, Ne, Lt, Le, Gt, Ge, Neg, Not, Print, Return:
			limit = 1 // These opcodes have no index operand.
		default:
			return &ValidationError{Source: instruction.Source, Detail: "unknown bytecode opcode"}
		}
		if instruction.Index < 0 || instruction.Index >= limit || instruction.Kind != 0 {
			return &ValidationError{Source: instruction.Source, Detail: "invalid instruction operand"}
		}
	}
	return nil
}

type flowState struct {
	stack       []ir.Kind
	initialized []bool
}

func validateFlow(fn Function, program Program) error {
	// Encoding validation has already checked every branch target. A block
	// starts at the entry, a branch target, or just after a control transfer.
	leaders := make([]bool, len(fn.Code))
	leaders[0] = true
	for pc, instruction := range fn.Code {
		switch instruction.Op {
		case Jump, JumpIfFalse:
			leaders[instruction.Index] = true
		case Return:
		default:
			continue
		}
		if pc+1 < len(fn.Code) {
			leaders[pc+1] = true
		}
	}

	initial := flowState{initialized: make([]bool, len(fn.Locals))}
	for index := range fn.Params {
		initial.initialized[index] = true
	}
	states := map[int]*flowState{0: &initial}
	queue := []int{0}
	for len(queue) > 0 {
		pc := queue[0]
		queue = queue[1:]
		prev := states[pc]
		next := flowState{stack: slices.Clone(prev.stack), initialized: slices.Clone(prev.initialized)}
		// Advance one working state through the block. Only entries need
		// snapshots for joins and the fixed-point worklist.
		for {
			if err := next.apply(fn, program, fn.Code[pc]); err != nil {
				return err
			}
			if pc+1 == len(fn.Code) || leaders[pc+1] {
				break
			}
			pc++
		}
		instruction := fn.Code[pc]
		if instruction.Op == Return {
			continue
		}
		successors := [2]int{pc + 1}
		count := 1
		switch instruction.Op {
		case Jump:
			successors[0] = instruction.Index
		case JumpIfFalse:
			successors[1] = instruction.Index
			count = 2
		}
		for _, target := range successors[:count] {
			if target >= len(fn.Code) {
				return &ValidationError{Source: instruction.Source, Detail: "control flow falls through the end of the function"}
			}
			joined := states[target]
			if joined == nil {
				state := flowState{stack: slices.Clone(next.stack), initialized: slices.Clone(next.initialized)}
				states[target] = &state
				queue = append(queue, target)
				continue
			}
			if !slices.Equal(joined.stack, next.stack) {
				return &ValidationError{Source: fn.Code[target].Source, Detail: "stack shape differs across control-flow paths"}
			}
			changed := false
			for index, initialized := range next.initialized {
				if joined.initialized[index] && !initialized {
					joined.initialized[index] = false
					changed = true
				}
			}
			if changed {
				queue = append(queue, target)
			}
		}
	}
	return nil
}

func (s *flowState) apply(fn Function, program Program, instruction Instruction) error {
	badStack := &ValidationError{Source: instruction.Source, Detail: "incorrect operand types or stack underflow"}
	switch instruction.Op {
	case Const:
		s.stack = append(s.stack, instruction.Kind)
	case Load:
		if !s.initialized[instruction.Index] {
			return &ValidationError{Source: instruction.Source, Detail: "read of an uninitialized local"}
		}
		s.stack = append(s.stack, fn.Locals[instruction.Index])
	case Store:
		if !s.pop(fn.Locals[instruction.Index]) {
			return badStack
		}
		s.initialized[instruction.Index] = true
	case Add, Sub, Mul, Div, Mod, Lt, Le, Gt, Ge:
		if !s.popPair(ir.Int) {
			return badStack
		}
		kind := ir.Int
		if instruction.Op == Lt || instruction.Op == Le || instruction.Op == Gt || instruction.Op == Ge {
			kind = ir.Bool
		}
		s.stack = append(s.stack, kind)
	case Eq, Ne:
		if len(s.stack) < 2 {
			return badStack
		}
		kind := s.stack[len(s.stack)-1]
		if (kind != ir.Int && kind != ir.Bool) || !s.popPair(kind) {
			return badStack
		}
		s.stack = append(s.stack, ir.Bool)
	case Neg, Not:
		kind := ir.Int
		if instruction.Op == Not {
			kind = ir.Bool
		}
		if !s.pop(kind) {
			return badStack
		}
		s.stack = append(s.stack, kind)
	case Print:
		if len(s.stack) == 0 || !ir.IsPrimitiveValue(s.stack[len(s.stack)-1]) {
			return badStack
		}
		s.stack = s.stack[:len(s.stack)-1]
	case MakeRecord:
		record, _ := ir.RecordFor(program.Records, instruction.Kind)
		base := len(s.stack) - len(record.Fields)
		if base < 0 || !slices.Equal(s.stack[base:], record.Fields) {
			return badStack
		}
		s.stack = append(s.stack[:base], instruction.Kind)
	case GetField:
		if !s.pop(instruction.Kind) {
			return badStack
		}
		record, _ := ir.RecordFor(program.Records, instruction.Kind)
		s.stack = append(s.stack, record.Fields[instruction.Index])
	case JumpIfFalse:
		if !s.pop(ir.Bool) {
			return badStack
		}
	case Call:
		callee := program.Functions[instruction.Index]
		if len(s.stack) < len(callee.Params) {
			return badStack
		}
		base := len(s.stack) - len(callee.Params)
		if !slices.Equal(s.stack[base:], callee.Params) {
			return badStack
		}
		s.stack = s.stack[:base]
		if callee.Result != ir.Void {
			s.stack = append(s.stack, callee.Result)
		}
	case Return:
		if (fn.Result != ir.Void && !s.pop(fn.Result)) || len(s.stack) != 0 {
			return &ValidationError{Source: instruction.Source, Detail: "return stack does not match function result"}
		}
	}
	return nil
}

func (s *flowState) pop(kind ir.Kind) bool {
	v := len(s.stack) - 1
	if v < 0 || s.stack[v] != kind {
		return false
	}
	s.stack = s.stack[:v]
	return true
}

func (s *flowState) popPair(kind ir.Kind) bool {
	base := len(s.stack) - 2
	if base < 0 || s.stack[base] != kind || s.stack[base+1] != kind {
		return false
	}
	s.stack = s.stack[:base]
	return true
}
