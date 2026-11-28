package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gosplash.dev/splash/internal/compiler"
	"gosplash.dev/splash/internal/effects"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "run" {
		if err := runVMCommand(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "inspect", "summary":
			run := runInspectCommand
			if os.Args[1] == "summary" {
				run = runSummaryCommand
			}
			ok, err := run(os.Args[2:], os.Stdout)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			if !ok {
				os.Exit(1)
			}
			return
		}
	}
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: splash <check|inspect|summary|callers|callees|authority|run|build|emit|tools|graph|effects|approvals> <file.splash>")
		os.Exit(1)
	}
	cmd, file := os.Args[1], os.Args[2]

	switch cmd {
	case "authority":
		ok, err := runAuthorityCommand(os.Args[2:], os.Stdout)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		if !ok {
			os.Exit(1)
		}
	case "callers", "callees":
		ok, err := runDependencyCommand(compiler.DependencyDirection(cmd), os.Args[2:], os.Stdout)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		if !ok {
			os.Exit(1)
		}
	case "check":
		path, jsonMode, err := parseJSONFileArgs(cmd, os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if jsonMode {
			ok, err := runCheckJSON(path, os.Stdout)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			if !ok {
				os.Exit(1)
			}
			return
		}
		if err := runCheck(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "emit":
		path, jsonMode, err := parseJSONFileArgs(cmd, os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if jsonMode {
			err = runEmitJSON(path, os.Stdout)
		} else {
			err = runEmit(path)
		}
		if err != nil {
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
		format := compiler.FormatOpenAI // default
		toolFile := ""
		args := os.Args[2:]
		for i := 0; i < len(args); i++ {
			if args[i] == "--format" {
				if i+1 >= len(args) {
					fmt.Fprintln(os.Stderr, "--format requires a value: anthropic or openai")
					os.Exit(1)
				}
				format = compiler.ToolFormat(args[i+1])
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

func analyzeProgram(path string) (*compiler.AnalyzedProgram, error) {
	program, diagnostics, err := compiler.Analyze(path)
	for _, diagnostic := range diagnostics {
		fmt.Fprintln(os.Stderr, diagnostic)
	}
	return program, err
}

func verifyProgram(program *compiler.AnalyzedProgram) (*compiler.VerifiedProgram, error) {
	verified, result, err := compiler.Verify(program)
	for _, diagnostic := range result.Violations {
		fmt.Fprintln(os.Stderr, diagnostic)
	}
	for _, diagnostic := range result.Incomplete {
		fmt.Fprintln(os.Stderr, diagnostic)
	}
	return verified, err
}

func runCheck(path string) error {
	program, err := analyzeProgram(path)
	if err != nil {
		return err
	}

	if _, err := verifyProgram(program); err != nil {
		return fmt.Errorf("check failed: %w", err)
	}
	fmt.Printf("%s: ok\n", path)
	return nil
}

func runEmit(path string) error {
	program, err := analyzeProgram(path)
	if err != nil {
		return err
	}

	verified, err := verifyProgram(program)
	if err != nil {
		return err
	}
	goSrc, err := verified.EmitGo()
	if err != nil {
		return err
	}
	fmt.Print(goSrc)
	return nil
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

	verified, err := verifyProgram(program)
	if err != nil {
		return err
	}
	goSrc, err := verified.EmitExecutableGo()
	if err != nil {
		return err
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

func runTools(path string, format compiler.ToolFormat) error {
	program, err := analyzeProgram(path)
	if err != nil {
		return err
	}

	verified, err := verifyProgram(program)
	if err != nil {
		return fmt.Errorf("tool export: %w", err)
	}
	out, err := verified.ExportTools(format)
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

	roots := program.AgentRoots()

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
	for _, node := range program.Functions() {
		flags := ""
		if len(node.Flags) > 0 {
			flags = " [" + strings.Join(node.Flags, "] [") + "]"
		}
		fmt.Printf("- %s [effects: %s]%s\n", node.Name, node.Declared, flags)
		if node.UnresolvedCalls > 0 {
			fmt.Printf("  unresolved call sites: %d\n", node.UnresolvedCalls)
		}
		callees := node.Callees
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
	for _, summary := range program.Functions() {
		fmt.Printf("- %s: %s (declared)\n", summary.Name, summary.Declared)
		if summary.Gap != nil {
			if summary.Inferred == effects.None {
				fmt.Println("  inferred: unknown")
			} else {
				fmt.Printf("  inferred: %s (known)\n", summary.Inferred)
			}
			fmt.Printf("  coverage: incomplete (%s at %s)\n", summary.Gap.Reason, summary.Gap.Position)
			continue
		}
		fmt.Printf("  inferred: %s\n  coverage: complete\n", summary.Inferred)
		if unused := summary.Unused; unused != effects.None {
			fmt.Printf("  unused grants: %s\n", unused)
		}
	}
	return nil
}

func runApprovals(path string) error {
	program, err := analyzeProgram(path)
	if err != nil {
		return err
	}

	approveFns, callers := program.Approvals()

	fmt.Println("approval-gated functions:")
	if len(approveFns) == 0 {
		fmt.Println("- none")
	} else {
		for _, name := range approveFns {
			fmt.Printf("- %s\n", name)
		}
	}

	fmt.Println()
	fmt.Println("approval callers:")
	if len(callers) == 0 {
		fmt.Println("- none")
	} else {
		for _, name := range callers {
			fmt.Printf("- %s\n", name)
		}
	}
	return nil
}
