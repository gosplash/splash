package compiler

import (
	"fmt"
	"io"
	"strings"

	"gosplash.dev/splash/internal/bytecode"
	"gosplash.dev/splash/internal/compiler/internal/codegen"
	"gosplash.dev/splash/internal/compiler/internal/lowering"
	"gosplash.dev/splash/internal/compiler/internal/toolschema"
	"gosplash.dev/splash/internal/ir"
	"gosplash.dev/splash/internal/vm"
)

// ToolFormat selects the wire format for a tool manifest.
type ToolFormat = toolschema.Format

const (
	FormatOpenAI    = toolschema.FormatOpenAI
	FormatAnthropic = toolschema.FormatAnthropic
)

// VMAdmissionError describes a verified source form outside the VM subset.
type VMAdmissionError = lowering.AdmissionError

// EmitGo emits Go for the captured program with its original package name.
func (v *VerifiedProgram) EmitGo() (string, error) {
	return v.emitGo(false)
}

// EmitExecutableGo emits Go in package main for native executable builds.
func (v *VerifiedProgram) EmitExecutableGo() (string, error) {
	return v.emitGo(true)
}

func (v *VerifiedProgram) emitGo(executable bool) (string, error) {
	if v == nil || v.verified == nil {
		return "", ErrUnverifiedProgram
	}
	p := v.verified
	source := codegen.NewGoBackend().Emit(p.merged, codegen.Options{
		ApprovalCallers:    p.graph.Callers(p.graph.ApprovalFunctions()),
		FunctionKey:        p.graph.FunctionKey,
		FunctionReferences: p.functionReferences,
		PrintlnCalls:       p.printlnCalls,
		LocalNames:         p.localNames,
		Entry:              p.modules[len(p.modules)-1].File,
	})
	if module := p.merged.Module; executable && module != nil && module.Name != "main" {
		source = strings.Replace(source, "package "+module.Name+"\n", "package main\n", 1)
	}
	return source, nil
}

// LowerVM admits the captured program to the VM subset and produces fresh IR.
func (v *VerifiedProgram) LowerVM() (ir.Program, error) {
	if v == nil || v.verified == nil {
		return ir.Program{}, ErrUnverifiedProgram
	}
	return lowering.LowerVM(v.verified.modules)
}

// RunVM validates and executes the captured program through the VM.
func (v *VerifiedProgram) RunVM(output io.Writer, limits vm.Limits) error {
	program, err := v.LowerVM()
	if err != nil {
		return err
	}
	executable, err := bytecode.Lower(program)
	if err != nil {
		return err
	}
	runtime, err := vm.New(executable, output, limits)
	if err != nil {
		return err
	}
	return runtime.RunMain()
}

// ExportTools serializes the tool declarations in the captured program.
func (v *VerifiedProgram) ExportTools(format ToolFormat) ([]byte, error) {
	if v == nil || v.verified == nil {
		return nil, ErrUnverifiedProgram
	}
	schemas, err := toolschema.Extract(v.verified.modules)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool)
	for _, schema := range schemas {
		if names[schema.Name] {
			return nil, fmt.Errorf("duplicate exported tool name %q", schema.Name)
		}
		names[schema.Name] = true
	}
	return toolschema.Serialize(schemas, format)
}
