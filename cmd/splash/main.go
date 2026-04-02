package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/codegen"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
	"gosplash.dev/splash/internal/safety"
	"gosplash.dev/splash/internal/toolschema"
	"gosplash.dev/splash/internal/typechecker"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: splash <check|build|tools> <file.splash> [-o output]")
		os.Exit(1)
	}
	cmd, file := os.Args[1], os.Args[2]

	switch cmd {
	case "check":
		if err := runCheck(file); err != nil {
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
		if err := runTools(file); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

func parseFile(path string) (*ast.File, error) {
	src, err := os.ReadFile(path)
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
	return f, nil
}

func runCheck(path string) error {
	f, err := parseFile(path)
	if err != nil {
		return err
	}

	tc := typechecker.New()
	_, typeErrs := tc.Check(f)
	for _, d := range typeErrs {
		fmt.Fprintln(os.Stderr, d)
	}

	g := callgraph.Build(f)
	sc := safety.New()
	safetyErrs := sc.Check(f, g)
	for _, d := range safetyErrs {
		fmt.Fprintln(os.Stderr, d)
	}

	if len(typeErrs)+len(safetyErrs) > 0 {
		return fmt.Errorf("check failed")
	}
	fmt.Printf("%s: ok\n", path)
	return nil
}

func runBuild(path, out string) error {
	out, err := filepath.Abs(out)
	if err != nil {
		return err
	}

	f, err := parseFile(path)
	if err != nil {
		return err
	}

	tc := typechecker.New()
	_, typeErrs := tc.Check(f)
	for _, d := range typeErrs {
		fmt.Fprintln(os.Stderr, d)
	}
	if len(typeErrs) > 0 {
		return fmt.Errorf("type errors")
	}

	g := callgraph.Build(f)
	sc := safety.New()
	safetyErrs := sc.Check(f, g)
	for _, d := range safetyErrs {
		fmt.Fprintln(os.Stderr, d)
	}
	if len(safetyErrs) > 0 {
		return fmt.Errorf("safety errors")
	}

	e := codegen.New()
	goSrc := e.EmitFile(f)

	// splash build always produces an executable — package must be main
	if f.Module != nil && f.Module.Name != "main" {
		goSrc = strings.Replace(goSrc, "package "+f.Module.Name+"\n", "package main\n", 1)
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

func runTools(path string) error {
	f, err := parseFile(path)
	if err != nil {
		return err
	}

	tc := typechecker.New()
	_, typeErrs := tc.Check(f)
	for _, d := range typeErrs {
		fmt.Fprintln(os.Stderr, d)
	}
	if len(typeErrs) > 0 {
		return fmt.Errorf("type errors")
	}

	schemas := toolschema.Extract(f)
	if schemas == nil {
		schemas = []toolschema.ToolSchema{}
	}
	out, err := json.MarshalIndent(schemas, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
