package codegen

import (
	"bytes"
	"errors"
	"testing"

	"gosplash.dev/splash/internal/token"
)

func TestVM001ValidatesIRBeforeBytecodeLowering(t *testing.T) {
	for _, tc := range []struct {
		name string
		ops  []vmIROp
		end  vmIRTerminator
	}{
		{"unknown operation", []vmIROp{{kind: 255}}, vmIRReturn},
		{"invalid value type", []vmIROp{{kind: vmIRStringConst, typ: vmVoid}}, vmIRReturn},
		{"use before definition", []vmIROp{{kind: vmIRPrint, typ: vmString, value: 1}}, vmIRReturn},
		{"duplicate temporary", []vmIROp{{kind: vmIRStringConst, typ: vmString}, {kind: vmIRStringConst, typ: vmString}}, vmIRReturn},
		{"missing return", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := lowerVMBytecode(vmIR{result: vmVoid, entry: vmIRBlock{ops: tc.ops, terminator: tc.end}})
			var vmErr *VMError
			if !errors.As(err, &vmErr) || vmErr.Code != VMInvalidIR {
				t.Fatalf("want invalid IR, got %v", err)
			}
		})
	}
}

func TestVM001ValidatesBytecodeBeforeOutput(t *testing.T) {
	for _, tc := range []struct {
		name string
		code []vmInstruction
	}{
		{"invalid opcode after valid print", []vmInstruction{{op: vmConstString}, {op: vmPrint}, {op: 255}, {op: vmReturn}}},
		{"invalid constant index", []vmInstruction{{op: vmConstString, operand: 1}, {op: vmPrint}, {op: vmReturn}}},
		{"stack underflow", []vmInstruction{{op: vmPrint}, {op: vmReturn}}},
		{"nonempty stack on return", []vmInstruction{{op: vmConstString}, {op: vmReturn}}},
		{"missing return", []vmInstruction{{op: vmConstString}, {op: vmPrint}}},
		{"instruction after return", []vmInstruction{{op: vmReturn}, {op: vmReturn}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := executeVMBytecode(vmBytecode{constants: []string{"hello"}, code: tc.code}, &out, VMLimits{Instructions: 100, Frames: 1})
			var vmErr *VMError
			if !errors.As(err, &vmErr) || vmErr.Code != VMInvalidBytecode {
				t.Fatalf("want invalid bytecode, got %v", err)
			}
			if out.Len() != 0 {
				t.Fatalf("malformed bytecode produced output %q", out.String())
			}
		})
	}
}

func TestVM001EnforcesDeterministicLimits(t *testing.T) {
	pos := token.Position{File: "hello.splash", Line: 3, Column: 5}
	program := vmBytecode{
		constants: []string{"hello"},
		code: []vmInstruction{
			{op: vmConstString, source: pos}, {op: vmPrint, source: pos}, {op: vmReturn, source: pos},
		},
	}
	for _, tc := range []struct {
		name  string
		limit VMLimits
		code  VMErrorCode
		count uint64
		depth int
	}{
		{"instruction budget", VMLimits{Instructions: 1, Frames: 1}, VMInstructionLimit, 1, 1},
		{"entry frame budget", VMLimits{Instructions: 3, Frames: 0}, VMFrameLimit, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := executeVMBytecode(program, &out, tc.limit)
			var vmErr *VMError
			if !errors.As(err, &vmErr) || vmErr.Code != tc.code || vmErr.InstructionCount != tc.count || vmErr.FrameDepth != tc.depth || vmErr.Source != pos {
				t.Fatalf("want %s at %s with %d instructions and frame depth %d, got %#v", tc.code, pos, tc.count, tc.depth, err)
			}
			if out.Len() != 0 {
				t.Fatalf("exhausted execution produced output %q", out.String())
			}
		})
	}
}

func TestVM001PreservesLiteralAndPrintProvenance(t *testing.T) {
	literal := token.Position{File: "hello.splash", Line: 4, Column: 9}
	print := token.Position{File: "hello.splash", Line: 3, Column: 5}
	program, err := lowerVMBytecode(vmIR{result: vmVoid, entry: vmIRBlock{
		ops: []vmIROp{
			{kind: vmIRStringConst, typ: vmString, text: "hello", source: literal},
			{kind: vmIRPrint, typ: vmString, source: print},
		},
		terminator: vmIRReturn,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for count, want := range []token.Position{literal, print} {
		var out bytes.Buffer
		err := executeVMBytecode(program, &out, VMLimits{Instructions: uint64(count), Frames: 1})
		var vmErr *VMError
		if !errors.As(err, &vmErr) || vmErr.Code != VMInstructionLimit || vmErr.Source != want {
			t.Fatalf("want instruction budget failure at %s, got %v", want, err)
		}
	}
}

func TestVM001ReportsMissingOutputHook(t *testing.T) {
	program := vmBytecode{
		constants: []string{"hello"},
		code:      []vmInstruction{{op: vmConstString}, {op: vmPrint}, {op: vmReturn}},
	}
	err := executeVMBytecode(program, nil, VMLimits{Instructions: 3, Frames: 1})
	var vmErr *VMError
	if !errors.As(err, &vmErr) || vmErr.Code != VMOutputError {
		t.Fatalf("want output configuration error, got %v", err)
	}
}
