package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingEffectRejectsArtifactPaths(t *testing.T) {
	dir := t.TempDir()
	writeSplashFile(t, dir, "catalog.splash", "module catalog\nfn lookup() needs DB.read -> Int { return 42 }\n")
	path := writeSplashFile(t, dir, "main.splash", "module main\nuse catalog as rules\nfn main() { println(rules.lookup()) }\n")
	artifact := filepath.Join(dir, "program")
	for name, command := range map[string]func() error{
		"check": func() error { return runCheck(path) },
		"emit":  func() error { return runEmit(path) },
		"build": func() error { return runBuild(path, artifact) },
		"run":   func() error { return runVMCommand([]string{path, "--backend", "vm"}, os.Stdout) },
	} {
		t.Run(name, func(t *testing.T) {
			output, err := captureStdout(t, command)
			if err == nil || !strings.Contains(err.Error(), "type errors") || output != "" {
				t.Fatalf("want effect rejection in source analysis before output, got %v and %q", err, output)
			}
		})
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("want no artifact after rejection, got %v", err)
	}
}
