package main

import (
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/bytecode"
	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/codegen"
	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/effects"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/lowering"
	"gosplash.dev/splash/internal/parser"
	"gosplash.dev/splash/internal/safety"
	"gosplash.dev/splash/internal/toolschema"
	"gosplash.dev/splash/internal/typechecker"
	"gosplash.dev/splash/internal/vm"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: splash <check|build|emit|tools|graph|effects|approvals> <file.splash>")
		os.Exit(1)
	}
	cmd, file := os.Args[1], os.Args[2]

	switch cmd {
	case "check":
		if err := runCheck(file); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "emit":
		if err := runEmit(file); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "build":
		out := strings.TrimSuffix(filepath.Base(file), ".splash")
		for i := 3; i < len(os.Args)-1; i++ {
			if os.Args[i] == "-o" {
				out = os.Args[i+1]
			}
		}
		if err := runBuild(file, out); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "tools":
		format := toolschema.FormatOpenAI // default
		toolFile := ""
		args := os.Args[2:]
		for i := 0; i < len(args); i++ {
			if args[i] == "--format" {
				if i+1 >= len(args) {
					fmt.Fprintln(os.Stderr, "--format requires a value: anthropic or openai")
					os.Exit(1)
				}
				format = toolschema.Format(args[i+1])
				i++
			} else if !strings.HasPrefix(args[i], "-") {
				toolFile = args[i]
			}
		}
		if toolFile == "" {
			fmt.Fprintln(os.Stderr, "usage: splash tools <file.splash> [--format anthropic|openai]")
			os.Exit(1)
		}
		if err := runTools(toolFile, format); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "graph":
		if err := runGraph(file); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "effects":
		if err := runEffects(file); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "approvals":
		if err := runApprovals(file); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

// analyzedProgram owns the captured source, typed modules, and resolved calls.
type analyzedProgram struct {
	sources map[string]string
	modules []*typechecker.TypedFile
	merged  *ast.File
	graph   *callgraph.Graph

	functionReferences map[ast.Expr]*ast.FunctionDecl
	printlnCalls       map[*ast.CallExpr]bool
	localNames         map[string]bool
}

func analyzeProgram(path string) (*analyzedProgram, error) {
	program := &analyzedProgram{sources: make(map[string]string)}
	read := func(path string) ([]byte, error) {
		if source, ok := program.sources[path]; ok {
			return []byte(source), nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		program.sources[path] = string(data)
		return data, nil
	}

	src, err := read(path)
	if err != nil {
		return nil, err
	}
	toks := lexer.New(path, string(src)).Tokenize()
	p := parser.New(path, toks)
	f, diags := p.ParseFile()
	for _, d := range diags {
		fmt.Fprintln(os.Stderr, d)
	}
	if len(diags) > 0 {
		return nil, fmt.Errorf("parse errors in %s", path)
	}

	tc := typechecker.New()
	tc.SetFileLoader(filepath.Dir(path), read)
	entry, typeErrs := tc.Check(f)
	for _, d := range typeErrs {
		fmt.Fprintln(os.Stderr, d)
	}
	if len(typeErrs) > 0 {
		return nil, fmt.Errorf("type errors")
	}

	program.modules = append(tc.LoadedTypedFiles(), entry)
	program.merged = mergeFiles(program.modules)
	files := make([]*ast.File, 0, len(program.modules))
	program.functionReferences = make(map[ast.Expr]*ast.FunctionDecl)
	program.printlnCalls = make(map[*ast.CallExpr]bool)
	program.localNames = make(map[string]bool)
	unresolved := make(map[*ast.CallExpr]bool)
	for _, module := range program.modules {
		files = append(files, module.File)
		maps.Copy(program.functionReferences, module.FunctionReferences)
		maps.Copy(program.printlnCalls, module.PrintlnCalls)
		maps.Copy(program.localNames, module.LocalNames)
		maps.Copy(unresolved, module.UnresolvedCalls)
	}
	program.graph = callgraph.BuildResolved(files, program.functionReferences, unresolved)
	return program, nil
}

func runCheck(path string) error {
	program, err := analyzeProgram(path)
	if err != nil {
		return err
	}

	if program.reportSafetyErrors() {
		return fmt.Errorf("check failed")
	}
	fmt.Printf("%s: ok\n", path)
	return nil
}

func (p *analyzedProgram) safetyDiagnostics() []diagnostic.Diagnostic {
	return safety.New().Check(p.merged, p.graph)
}

func (p *analyzedProgram) reportSafetyErrors() bool {
	diagnostics := p.safetyDiagnostics()
	for _, d := range diagnostics {
		fmt.Fprintln(os.Stderr, d)
	}
	return len(diagnostics) > 0
}

func runEmit(path string) error {
	program, err := analyzeProgram(path)
	if err != nil {
		return err
	}

	goSrc, err := program.emitGo()
	if err != nil {
		return err
	}
	fmt.Print(goSrc)
	return nil
}

func (p *analyzedProgram) emitGo() (string, error) {
	if p.reportSafetyErrors() {
		return "", fmt.Errorf("safety errors")
	}
	return codegen.NewGoBackend().Emit(p.merged, codegen.Options{
		ApprovalCallers:    p.graph.Callers(p.graph.ApprovalFunctions()),
		FunctionKey:        p.graph.FunctionKey,
		FunctionReferences: p.functionReferences,
		PrintlnCalls:       p.printlnCalls,
		LocalNames:         p.localNames,
		Entry:              p.modules[len(p.modules)-1].File,
	}), nil
}

// runVM is the experimental internal path. Public commands still use Go lowering.
func (p *analyzedProgram) runVM(output io.Writer, limits vm.Limits) error {
	if p.reportSafetyErrors() {
		return fmt.Errorf("safety errors")
	}
	program, err := lowering.LowerVM(p.modules)
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

// mergeFiles builds a combined ast.File where all imported files' declarations
// come first (in load order), followed by the main file's declarations.
// The main file's module declaration and package name are preserved.
// This ensures imported types are defined before the main file references them
// in the generated Go output.
func mergeFiles(modules []*typechecker.TypedFile) *ast.File {
	main := modules[len(modules)-1].File
	if len(modules) == 1 {
		return main
	}
	merged := &ast.File{
		Module:   main.Module,
		Exposes:  main.Exposes,
		Position: main.Position,
	}
	for _, module := range modules {
		f := module.File
		merged.Uses = append(merged.Uses, f.Uses...)
		merged.Declarations = append(merged.Declarations, f.Declarations...)
	}
	return merged
}

func runBuild(path, out string) error {
	out, err := filepath.Abs(out)
	if err != nil {
		return err
	}

	program, err := analyzeProgram(path)
	if err != nil {
		return err
	}

	goSrc, err := program.emitGo()
	if err != nil {
		return err
	}

	// splash build always produces an executable — package must be main
	if module := program.merged.Module; module != nil && module.Name != "main" {
		goSrc = strings.Replace(goSrc, "package "+module.Name+"\n", "package main\n", 1)
	}

	tmpDir, err := os.MkdirTemp("", "splash-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(goSrc), 0644); err != nil {
		return err
	}
	goMod := "module splashbuild\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return err
	}

	buildCmd := exec.Command("go", "build", "-o", out, ".")
	buildCmd.Dir = tmpDir
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	return buildCmd.Run()
}

func runTools(path string, format toolschema.Format) error {
	program, err := analyzeProgram(path)
	if err != nil {
		return err
	}

	if program.reportSafetyErrors() {
		return fmt.Errorf("safety errors: tool surface is unsafe")
	}

	// Each tool declaration is an agent root, and the safety check above
	// rejects the entire export when the modeled graph violates policy.
	schemas := toolschema.Extract(program.merged)
	names := make(map[string]bool)
	for _, schema := range schemas {
		if names[schema.Name] {
			return fmt.Errorf("duplicate exported tool name %q", schema.Name)
		}
		names[schema.Name] = true
	}
	if schemas == nil {
		schemas = []toolschema.ToolSchema{}
	}
	out, err := toolschema.Serialize(schemas, format)
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func runGraph(path string) error {
	program, err := analyzeProgram(path)
	if err != nil {
		return err
	}

	g := program.graph
	roots := g.AgentRoots()

	fmt.Println("agent roots:")
	if len(roots) == 0 {
		fmt.Println("- none")
	} else {
		for _, root := range roots {
			fmt.Printf("- %s\n", root)
		}
	}

	fmt.Println()
	fmt.Println("functions:")
	for _, node := range g.Nodes() {
		fmt.Printf("- %s [effects: %s]%s\n", node.Name, node.Effects.String(), graphFlags(node))
		if len(node.UnresolvedCalls) > 0 {
			fmt.Printf("  unresolved call sites: %d\n", len(node.UnresolvedCalls))
		}
		callees := node.Callees()
		if len(callees) == 0 {
			fmt.Println("  calls: none")
			continue
		}
		fmt.Println("  calls:")
		for _, callee := range callees {
			fmt.Printf("  - %s\n", callee)
		}
	}

	return nil
}

func runEffects(path string) error {
	program, err := analyzeProgram(path)
	if err != nil {
		return err
	}

	fmt.Println("effects:")
	for _, node := range program.graph.Nodes() {
		fmt.Printf("- %s: %s\n", node.Name, node.Effects.String())
	}
	return nil
}

func runApprovals(path string) error {
	program, err := analyzeProgram(path)
	if err != nil {
		return err
	}

	approveFns := program.graph.ApprovalFunctions()
	callers := program.graph.Callers(approveFns)

	fmt.Println("approval-gated functions:")
	if len(approveFns) == 0 {
		fmt.Println("- none")
	} else {
		for _, name := range sortedKeys(approveFns) {
			fmt.Printf("- %s\n", name)
		}
	}

	fmt.Println()
	fmt.Println("approval callers:")
	if len(callers) == 0 {
		fmt.Println("- none")
	} else {
		for _, name := range sortedKeys(callers) {
			fmt.Printf("- %s\n", name)
		}
	}
	return nil
}

func graphFlags(node *callgraph.Node) string {
	var flags []string
	if node.Effects.Has(effects.Agent) {
		flags = append(flags, "agent")
	}
	if node.HasAnnotation(ast.AnnotTool) {
		flags = append(flags, "tool")
	}
	if node.HasAnnotation(ast.AnnotApprove) {
		flags = append(flags, "approve")
	}
	if node.HasAnnotation(ast.AnnotRedline) {
		flags = append(flags, "redline")
	}
	if len(flags) == 0 {
		return ""
	}
	return " [" + strings.Join(flags, "] [") + "]"
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
