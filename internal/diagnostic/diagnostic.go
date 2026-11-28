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

type Code string

const (
	ParseError                     Code = "PARSE_ERROR"
	TypeError                      Code = "TYPE_ERROR"
	TypeMismatch                   Code = "TYPE_MISMATCH"
	PolicyViolation                Code = "POLICY_VIOLATION"
	PolicyCoverageIncomplete       Code = "POLICY_COVERAGE_INCOMPLETE"
	DeclaredEffectMissing          Code = "DECLARED_EFFECT_MISSING"
	InferredEffectExceedsAuthority Code = "INFERRED_EFFECT_EXCEEDS_AUTHORITY"
)

// WitnessLimit is the maximum number of entries rendered per diagnostic path.
const WitnessLimit = 8

// WitnessStep is an edge at a source site, ending in a resolved declaration or
// modeled operation. End is exclusive. Declaration is set for resolved calls.
type WitnessStep struct {
	Target      string
	Site        token.Position
	End         token.Position
	Declaration token.Position
}

type Diagnostic struct {
	Code             Code
	PolicyRuleID     string
	WitnessLength    int
	WitnessTruncated bool
	End              token.Position
	Effects          []string
	Witness          []WitnessStep
	ExpectedType     string
	ActualType       string
	Severity         Severity
	Message          string
	Pos              token.Position
	Notes            []string
}

func (d Diagnostic) WithCode(code Code) Diagnostic {
	d.Code = code
	return d
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
