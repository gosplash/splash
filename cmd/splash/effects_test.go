package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/compiler"
	"gosplash.dev/splash/internal/safety"
)

func TestMissingEffectRejectsArtifactPaths(t *testing.T) {
	dir := t.TempDir()
	writeSplashFile(t, dir, "catalog.splash", "module catalog\nfn lookup() needs DB.read -> Int { return 42 }\n")
	path := writeSplashFile(t, dir, "main.splash", "module main\nuse catalog as rules\nfn main() { println(rules.lookup()) }\n")
	artifact := filepath.Join(dir, "program.exe")
	for name, command := range map[string]func() error{
		"check": func() error { return runCheck(path) },
		"emit":  func() error { return runEmit(path) },
		"build": func() error { return runBuild(path, artifact) },
		"run":   func() error { return runVMCommand([]string{path, "--backend", "vm"}, os.Stdout) },
		"tools": func() error { return runTools(path, compiler.FormatOpenAI) },
	} {
		t.Run(name, func(t *testing.T) {
			output, diag, err := captureEffectCommand(t, command)
			if !errors.Is(err, safety.ErrRejected) || output != "" {
				t.Fatalf("want effect rejection before output, got %v and %q", err, output)
			}
			if !strings.Contains(diag, `missing declared effect DB.read for call to "lookup"`) {
				t.Fatalf("want specific effect diagnostic, got %q", diag)
			}
		})
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("want no artifact after rejection, got %v", err)
	}

	writeSplashFile(t, dir, "main.splash", "module main\nuse catalog as rules\nfn main() needs DB.read { println(rules.lookup()) }\n")
	if err := runBuild(path, artifact); err != nil {
		t.Fatalf("build with explicit grant: %v", err)
	}
	if output, err := exec.Command(artifact).CombinedOutput(); err != nil || string(output) != "42\n" {
		t.Fatalf("run granted program: %q, %v", output, err)
	}
}

func TestQualifiedEffectCallRespectsSandbox(t *testing.T) {
	dir := t.TempDir()
	writeSplashFile(t, dir, "catalog.splash", "module catalog\nfn lookup() needs DB.read -> Int { return 42 }\n")
	const source = "module main\nuse catalog as rules\n%s\nfn main() needs DB.read { println(rules.lookup()) }\n"
	path := writeSplashFile(t, dir, "main.splash", fmt.Sprintf(source, "@sandbox(deny: [DB.read])"))
	output, diag, err := captureEffectCommand(t, func() error { return runCheck(path) })
	if !errors.Is(err, safety.ErrRejected) || output != "" ||
		!strings.Contains(diag, `reachable function "lookup" uses denied effect DB.read`) {
		t.Fatalf("want safety-stage rejection naming the imported callee, got %v, stdout %q, stderr %q", err, output, diag)
	}
	writeSplashFile(t, dir, "main.splash", fmt.Sprintf(source, "@sandbox(allow: [DB.read])"))
	if err := runCheck(path); err != nil {
		t.Fatalf("check qualified call with matching grant and policy: %v", err)
	}
}

func captureEffectCommand(t *testing.T, command func() error) (stdout, stderr string, err error) {
	t.Helper()
	file, createErr := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if createErr != nil {
		t.Fatal(createErr)
	}
	previous := os.Stderr
	os.Stderr = file
	defer func() {
		os.Stderr = previous
		file.Close()
	}()
	stdout, err = captureStdout(t, command)
	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		t.Fatal(seekErr)
	}
	message, readErr := io.ReadAll(file)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return stdout, string(message), err
}

func TestSandboxEffectPrefilterKeepsReachableViolations(t *testing.T) {
	binary := buildSplashCLI(t)
	dir := t.TempDir()
	for _, tc := range []struct {
		name, declaration, body, verdict string
	}{
		{"no forbidden effects", "DB.read", "return 1", "verified"},
		{"unrelated forbidden effect", "DB.read", "return 1", "verified"},
		{"reachable forbidden effect", "Net", "return network()", "rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "module main\n@sandbox(deny: [Net])\nfn root() needs " + tc.declaration + " -> Int { " + tc.body + " }\n"
			if tc.name != "no forbidden effects" {
				source += "fn network() needs Net -> Int { return 2 }\n"
			}
			path := writeSplashFile(t, dir, "main.splash", source)
			diagnostics, _ := readCheckJSON(t, binary, path, tc.verdict)
			if tc.verdict == "rejected" && (len(diagnostics) != 2 ||
				!strings.Contains(diagnostics[0].Message, "uses denied effect Net") ||
				!strings.Contains(diagnostics[1].Message, "uses denied effect Net")) {
				t.Fatalf("expected violations at the sandbox root and its callee, got %+v", diagnostics)
			}
		})
	}
}
