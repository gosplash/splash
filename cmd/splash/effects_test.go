package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/toolschema"
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
		"tools": func() error { return runTools(path, toolschema.FormatOpenAI) },
	} {
		t.Run(name, func(t *testing.T) {
			stderr, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
			if err != nil {
				t.Fatal(err)
			}
			previous := os.Stderr
			os.Stderr = stderr
			t.Cleanup(func() {
				os.Stderr = previous
				stderr.Close()
			})
			output, err := captureStdout(t, command)
			if err == nil || !strings.Contains(err.Error(), "type errors") || output != "" {
				t.Fatalf("want effect rejection in source analysis before output, got %v and %q", err, output)
			}
			if _, err := stderr.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			diag, err := io.ReadAll(stderr)
			if err != nil || !strings.Contains(string(diag), `missing declared effect DB.read for call to "lookup"`) {
				t.Fatalf("want specific effect diagnostic, got %q (%v)", diag, err)
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
	output, err := captureStdout(t, func() error { return runCheck(path) })
	if err == nil || err.Error() != "check failed" || output != "" {
		t.Fatalf("want safety-stage rejection, got %v and %q", err, output)
	}
	writeSplashFile(t, dir, "main.splash", fmt.Sprintf(source, "@sandbox(allow: [DB.read])"))
	if err := runCheck(path); err != nil {
		t.Fatalf("check qualified call with matching grant and policy: %v", err)
	}
}
