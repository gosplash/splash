package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func buildSplashCLI(t *testing.T) string {
	t.Helper()
	name := "splash"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build Splash CLI: %v\n%s", err, output)
	}
	return binary
}

func TestRunVMCli(t *testing.T) {
	binary := buildSplashCLI(t)
	workspace := t.TempDir()
	emptyPath := t.TempDir()
	t.Run("help", func(t *testing.T) {
		output, err := exec.Command(binary, "run", "--help").CombinedOutput()
		if err != nil || !strings.Contains(string(output), "--backend vm") ||
			!strings.Contains(string(output), "1000000") || !strings.Contains(string(output), "256") {
			t.Fatalf("run help = %q, %v", output, err)
		}
	})
	const hello = `println("hello")`
	analyticError := `let value: Int = "no"`
	for _, test := range []struct {
		name, body        string
		options           []string
		stdout, wantError string
	}{
		{name: "without Go", body: hello, stdout: "hello\n"},
		{name: "required backend", body: hello, options: []string{}, wantError: "--backend vm"},
		{name: "unknown backend", body: hello, options: []string{"--backend", "go"}, wantError: "unsupported backend"},
		{name: "extra argument", body: hello, options: []string{"--backend", "vm", "extra"}, wantError: "unexpected argument"},
		{name: "negative limit", body: hello, options: []string{"--backend", "vm", "--max-frames", "-1"}, wantError: "--max-frames"},
		{name: "source analysis", body: analyticError, wantError: "source analysis:"},
		{name: "admission", body: "println(1.5)", wantError: "SPLASH_VM_UNSUPPORTED_SOURCE"},
		{name: "IR validation", body: "println(1 + true)", wantError: "SPLASH_VM_INVALID_IR"},
		{name: "runtime", body: "println(4 / 0)", wantError: "SPLASH_RUNTIME_DIVISION_BY_ZERO"},
		{name: "instruction limit", body: hello, options: []string{"--backend", "vm", "--max-instructions", "0"}, wantError: "SPLASH_VM_INSTRUCTION_LIMIT"},
		{name: "frame limit", body: hello, options: []string{"--backend", "vm", "--max-frames", "0"}, wantError: "SPLASH_VM_FRAME_LIMIT"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeSplashFile(t, workspace, "main.splash", "module main\nfn main() {\n"+test.body+"\n}\n")
			options := test.options
			if options == nil {
				options = []string{"--backend", "vm"}
			}
			command := exec.Command(binary, append([]string{"run", path}, options...)...)
			command.Env = append(os.Environ(), "PATH="+emptyPath)
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			err := command.Run()
			if test.wantError == "" {
				if err != nil || stderr.Len() != 0 {
					t.Fatalf("run returned %v and stderr %q", err, stderr.String())
				}
			} else if err == nil || !strings.Contains(stderr.String(), test.wantError) {
				t.Fatalf("want error containing %q, got %v and stderr %q", test.wantError, err, stderr.String())
			}
			if stdout.String() != test.stdout {
				t.Fatalf("stdout = %q, want %q", stdout.String(), test.stdout)
			}
		})
	}
}
