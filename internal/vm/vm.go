// Package vm executes validated Splash bytecode with explicit per-run limits.
package vm

import (
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"unicode/utf8"

	"gosplash.dev/splash/internal/bytecode"
	"gosplash.dev/splash/internal/ir"
	"gosplash.dev/splash/internal/token"
)

type ErrorCode string

const (
	InvalidBytecode   ErrorCode = "SPLASH_VM_INVALID_BYTECODE"
	InvalidInvocation ErrorCode = "SPLASH_VM_INVALID_INVOCATION"
	InstructionLimit  ErrorCode = "SPLASH_VM_INSTRUCTION_LIMIT"
	FrameLimit        ErrorCode = "SPLASH_VM_FRAME_LIMIT"
	OutputError       ErrorCode = "SPLASH_VM_OUTPUT_ERROR"
	IntegerOverflow   ErrorCode = "SPLASH_RUNTIME_INTEGER_OVERFLOW"
	DivisionByZero    ErrorCode = "SPLASH_RUNTIME_DIVISION_BY_ZERO"
)

type Error struct {
	Code             ErrorCode
	Source           token.Position
	InstructionCount uint64
	FrameDepth       int
	Detail           string
}

func (e *Error) Error() string {
	if e.Source.File == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Detail)
	}
	return fmt.Sprintf("%s: %s: %s", e.Source, e.Code, e.Detail)
}

// Limits applies to each invocation. Zero grants no instructions or frames.
type Limits struct {
	Instructions uint64
	Frames       int
}

type Value struct {
	Kind ir.Kind
	Int  int64
	Bool bool
	Text string
}

// Runtime owns a validated program snapshot and the host-provided output
// writer. Invocations have independent frames, stacks and resource counters.
type Runtime struct {
	program bytecode.Program
	output  io.Writer
	limits  Limits
}

func New(program bytecode.Program, output io.Writer, limits Limits) (*Runtime, error) {
	snapshot := bytecode.Program{Entry: program.Entry, Functions: slices.Clone(program.Functions)}
	for index := range snapshot.Functions {
		fn := &snapshot.Functions[index]
		fn.Params = slices.Clone(fn.Params)
		fn.Locals = slices.Clone(fn.Locals)
		fn.Code = slices.Clone(fn.Code)
	}
	if err := bytecode.Validate(snapshot); err != nil {
		result := &Error{Code: InvalidBytecode, Detail: err.Error()}
		var validation *bytecode.ValidationError
		if errors.As(err, &validation) {
			result.Source = validation.Source
			result.Detail = validation.Detail
		}
		return nil, result
	}
	return &Runtime{program: snapshot, output: output, limits: limits}, nil
}

type frame struct {
	function int
	pc       int
	locals   []Value
	stack    []Value
}

func (f *frame) pop() Value {
	index := len(f.stack) - 1
	value := f.stack[index]
	f.stack[index] = Value{}
	f.stack = f.stack[:index]
	return value
}

func (runtime *Runtime) RunMain() error {
	entry := runtime.program.Entry
	if entry < 0 || entry >= len(runtime.program.Functions) {
		return &Error{Code: InvalidInvocation, Detail: "program has no main entry"}
	}
	_, err := runtime.Invoke(entry, nil)
	return err
}

func (runtime *Runtime) Invoke(functionID int, args []Value) (Value, error) {
	if functionID < 0 || functionID >= len(runtime.program.Functions) {
		return Value{}, &Error{Code: InvalidInvocation, Detail: "unknown function"}
	}
	fn := runtime.program.Functions[functionID]
	if len(args) != len(fn.Params) {
		return Value{}, &Error{Code: InvalidInvocation, Source: fn.Source, Detail: "argument count does not match function signature"}
	}
	for index, argument := range args {
		if argument.Kind != fn.Params[index] {
			return Value{}, &Error{Code: InvalidInvocation, Source: fn.Source, Detail: "argument type does not match function signature"}
		}
		if argument.Kind == ir.String && !utf8.ValidString(argument.Text) {
			return Value{}, &Error{Code: InvalidInvocation, Source: fn.Source, Detail: "string argument must be valid UTF-8"}
		}
	}
	if runtime.limits.Frames < 1 {
		return Value{}, &Error{Code: FrameLimit, Source: fn.Code[0].Source, Detail: "entry frame exceeds budget"}
	}

	frames := []frame{makeFrame(functionID, fn, args)}
	var dispatched uint64
	for {
		current := &frames[len(frames)-1]
		active := runtime.program.Functions[current.function]
		instruction := active.Code[current.pc]
		fail := func(code ErrorCode, detail string) (Value, error) {
			return Value{}, &Error{
				Code: code, Source: instruction.Source, InstructionCount: dispatched, FrameDepth: len(frames), Detail: detail,
			}
		}
		if dispatched == runtime.limits.Instructions {
			return fail(InstructionLimit, "instruction budget exhausted")
		}
		dispatched++
		current.pc++
		switch instruction.Op {
		case bytecode.Const:
			current.stack = append(current.stack, Value{Kind: instruction.Kind, Int: instruction.Int, Bool: instruction.Bool, Text: instruction.Text})
		case bytecode.Load:
			current.stack = append(current.stack, current.locals[instruction.Index])
		case bytecode.Store:
			current.locals[instruction.Index] = current.pop()
		case bytecode.Add, bytecode.Sub, bytecode.Mul, bytecode.Div, bytecode.Mod:
			right, left := current.pop(), current.pop()
			result, code := checkedArithmetic(instruction.Op, left.Int, right.Int)
			if code == DivisionByZero {
				return fail(code, "zero divisor")
			}
			if code != "" {
				return fail(code, "signed 64-bit arithmetic overflow")
			}
			current.stack = append(current.stack, Value{Kind: ir.Int, Int: result})
		case bytecode.Neg:
			value := current.pop().Int
			if value == math.MinInt64 {
				return fail(IntegerOverflow, "signed 64-bit negation overflow")
			}
			current.stack = append(current.stack, Value{Kind: ir.Int, Int: -value})
		case bytecode.Not:
			value := !current.pop().Bool
			current.stack = append(current.stack, Value{Kind: ir.Bool, Bool: value})
		case bytecode.Eq, bytecode.Ne, bytecode.Lt, bytecode.Le, bytecode.Gt, bytecode.Ge:
			right, left := current.pop(), current.pop()
			current.stack = append(current.stack, Value{Kind: ir.Bool, Bool: compare(instruction.Op, left, right)})
		case bytecode.Print:
			if runtime.output == nil {
				return fail(OutputError, "output hook is missing")
			}
			line := printValue(current.pop()) + "\n"
			written, err := io.WriteString(runtime.output, line)
			if err != nil {
				return fail(OutputError, err.Error())
			}
			if written != len(line) {
				return fail(OutputError, io.ErrShortWrite.Error())
			}
		case bytecode.Jump:
			current.pc = instruction.Index
		case bytecode.JumpIfFalse:
			if !current.pop().Bool {
				current.pc = instruction.Index
			}
		case bytecode.Call:
			if len(frames) >= runtime.limits.Frames {
				return fail(FrameLimit, "call frame exceeds budget")
			}
			callee := runtime.program.Functions[instruction.Index]
			base := len(current.stack) - len(callee.Params)
			next := makeFrame(instruction.Index, callee, current.stack[base:])
			clear(current.stack[base:])
			current.stack = current.stack[:base]
			frames = append(frames, next)
		case bytecode.Return:
			result := Value{Kind: ir.Void}
			if active.Result != ir.Void {
				result = current.pop()
			}
			*current = frame{}
			frames = frames[:len(frames)-1]
			if len(frames) == 0 {
				return result, nil
			}
			if result.Kind != ir.Void {
				caller := &frames[len(frames)-1]
				caller.stack = append(caller.stack, result)
			}
		}
	}
}

func makeFrame(id int, function bytecode.Function, args []Value) frame {
	locals := make([]Value, len(function.Locals))
	copy(locals, args)
	return frame{function: id, locals: locals}
}

func checkedArithmetic(op bytecode.Op, left, right int64) (int64, ErrorCode) {
	switch op {
	case bytecode.Add:
		result := left + right
		if right > 0 && result < left || right < 0 && result > left {
			return 0, IntegerOverflow
		}
		return result, ""
	case bytecode.Sub:
		result := left - right
		if right < 0 && result < left || right > 0 && result > left {
			return 0, IntegerOverflow
		}
		return result, ""
	case bytecode.Mul:
		if left == -1 && right == math.MinInt64 || right == -1 && left == math.MinInt64 {
			return 0, IntegerOverflow
		}
		result := left * right
		if right != 0 && result/right != left {
			return 0, IntegerOverflow
		}
		return result, ""
	case bytecode.Div:
		if right == 0 {
			return 0, DivisionByZero
		}
		if left == math.MinInt64 && right == -1 {
			return 0, IntegerOverflow
		}
		return left / right, ""
	case bytecode.Mod:
		if right == 0 {
			return 0, DivisionByZero
		}
		return left % right, ""
	}
	panic("invalid arithmetic opcode after bytecode validation")
}

func compare(op bytecode.Op, left, right Value) bool {
	switch op {
	case bytecode.Eq, bytecode.Ne:
		equal := left.Int == right.Int
		if left.Kind == ir.Bool {
			equal = left.Bool == right.Bool
		}
		return equal == (op == bytecode.Eq)
	case bytecode.Lt:
		return left.Int < right.Int
	case bytecode.Le:
		return left.Int <= right.Int
	case bytecode.Gt:
		return left.Int > right.Int
	case bytecode.Ge:
		return left.Int >= right.Int
	}
	panic("invalid comparison opcode after bytecode validation")
}

func printValue(value Value) string {
	switch value.Kind {
	case ir.Int:
		return strconv.FormatInt(value.Int, 10)
	case ir.Bool:
		return strconv.FormatBool(value.Bool)
	case ir.String:
		return value.Text
	}
	panic("invalid print operand after bytecode validation")
}
