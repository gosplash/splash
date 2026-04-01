package diagnostic_test

import (
	"testing"
	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/token"
)

func TestDiagnosticString(t *testing.T) {
	d := diagnostic.Diagnostic{
		Severity: diagnostic.Error,
		Message:  "cannot interpolate @sensitive value",
		Pos:      token.Position{File: "foo.splash", Line: 8, Column: 3},
	}
	got := d.String()
	want := "foo.splash:8:3: error: cannot interpolate @sensitive value"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDiagnosticWarning(t *testing.T) {
	d := diagnostic.Diagnostic{
		Severity: diagnostic.Warning,
		Message:  "unused variable",
		Pos:      token.Position{File: "bar.splash", Line: 1, Column: 5},
	}
	got := d.String()
	want := "bar.splash:1:5: warning: unused variable"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
