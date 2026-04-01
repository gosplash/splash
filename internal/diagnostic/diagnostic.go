package diagnostic

import (
	"fmt"
	"gosplash.dev/splash/internal/token"
)

type Severity int

const (
	Error Severity = iota
	Warning
	Note
)

func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	case Note:
		return "note"
	default:
		return "unknown"
	}
}

type Diagnostic struct {
	Severity Severity
	Message  string
	Pos      token.Position
	Notes    []string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%s: %s: %s", d.Pos, d.Severity, d.Message)
}

func Errorf(pos token.Position, format string, args ...any) Diagnostic {
	return Diagnostic{
		Severity: Error,
		Message:  fmt.Sprintf(format, args...),
		Pos:      pos,
	}
}
