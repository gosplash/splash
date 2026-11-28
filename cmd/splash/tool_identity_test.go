package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestToolSchemasKeepResolvedNominalIdentity(t *testing.T) {
	binary := buildSplashCLI(t)
	dir := t.TempDir()
	writeSplashFile(t, dir, "calendar.splash", `module calendar
expose Period
enum Period {
    morning
    evening
}
tool fn calendar_options(value: Period, alternatives: List<Period>) -> Int { return 1 }
`)
	writeSplashFile(t, dir, "freight.splash", `module freight
expose unrelated
enum Period {
    pickup
    delivery
}
fn unrelated() {}
tool fn freight_options(value: Period) -> Int { return 1 }
`)
	for _, imports := range []string{"use calendar as cal\nuse freight as delivery", "use freight as delivery\nuse calendar as cal"} {
		path := writeSplashFile(t, dir, "main.splash", "module main\n"+imports+`
type Period { duration: Int }
tool fn choose(value: cal.Period) -> Int { return 1 }
tool fn record_input(value: Period) -> Int { return 1 }
`)
		for _, format := range []string{"openai", "anthropic"} {
			output, err := exec.Command(binary, "tools", path, "--format", format).CombinedOutput()
			if err != nil {
				t.Fatalf("export tools: %v\n%s", err, output)
			}
			type property struct {
				Type  string
				Enum  []string
				Items *property
			}
			type parameters struct{ Properties map[string]property }
			var schemas []struct {
				Name        string
				InputSchema parameters `json:"input_schema"`
				Function    struct {
					Name       string
					Parameters parameters
				}
			}
			if err := json.Unmarshal(output, &schemas); err != nil {
				t.Fatalf("invalid schema JSON: %v\n%s", err, output)
			}
			got := make(map[string]map[string]property)
			for _, schema := range schemas {
				if format == "openai" {
					got[schema.Function.Name] = schema.Function.Parameters.Properties
				} else {
					got[schema.Name] = schema.InputSchema.Properties
				}
			}
			calendar := []string{"morning", "evening"}
			if !reflect.DeepEqual(got["choose"]["value"].Enum, calendar) ||
				!reflect.DeepEqual(got["calendar_options"]["value"].Enum, calendar) ||
				!reflect.DeepEqual(got["freight_options"]["value"].Enum, []string{"pickup", "delivery"}) {
				t.Fatalf("enum variants must follow their checked type:\n%s", output)
			}
			alternatives := got["calendar_options"]["alternatives"]
			if alternatives.Type != "array" || alternatives.Items == nil || !reflect.DeepEqual(alternatives.Items.Enum, calendar) ||
				got["record_input"]["value"].Type != "object" || len(got["record_input"]["value"].Enum) != 0 {
				t.Fatalf("list and record schemas must use their checked types:\n%s", output)
			}
		}
	}
}

func TestToolExportRejectsUnresolvedParameter(t *testing.T) {
	binary := buildSplashCLI(t)
	dir := t.TempDir()
	writeSplashFile(t, dir, "calendar.splash", `module calendar
expose helper
enum Period {
    morning
    evening
}
fn helper() {}
`)
	for _, typ := range []string{"cal.Period", "List<cal.Period>", "Int<Missing>", "List<String<Missing>>", "Empty", "Payload"} {
		path := writeSplashFile(t, dir, "main.splash", "module main\nuse calendar as cal\n"+`
enum Empty {}
enum Payload { item(Int) }
tool fn valid(value: Int) -> Int { return value }
`+"tool fn choose(value: "+typ+") -> Int { return 1 }\n")
		for _, format := range []string{"openai", "anthropic"} {
			command := exec.Command(binary, "tools", path, "--format", format)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			err := command.Run()
			if err == nil || stdout.Len() != 0 || !strings.Contains(stderr.String(), "parameter") {
				t.Fatalf("want parameter error before output for %s (%s), got %v, stdout %q, stderr %q", typ, format, err, &stdout, &stderr)
			}
		}
	}
}

func TestQualifiedEnumWithBuiltinNameKeepsGoSignature(t *testing.T) {
	binary := buildSplashCLI(t)
	dir := t.TempDir()
	writeSplashFile(t, dir, "calendar.splash", `module calendar
enum String {
    morning
    evening
}
`)
	entry := writeSplashFile(t, dir, "main.splash", `module main
use calendar as cal
tool fn choose(value: cal.String) -> Int { return 1 }
approve fn confirm(value: cal.String) -> cal.String { return value }
fn main() {}
`)
	output, err := exec.Command(binary, "emit", entry).CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte("func choose(value String) int")) {
		t.Fatalf("Go output must preserve the qualified nominal type: %v\n%s", err, output)
	}
	if output, err := exec.Command(binary, "build", entry, "-o", filepath.Join(dir, "program")).CombinedOutput(); err != nil {
		t.Fatalf("native build must preserve the nominal type on every approval path: %v\n%s", err, output)
	}
}

func TestToolExportRejectsEmptyTypeApplications(t *testing.T) {
	binary := buildSplashCLI(t)
	for _, typ := range []string{"Int<>", "List<Int<>>"} {
		entry := writeSplashFile(t, t.TempDir(), "main.splash",
			"module main\ntool fn choose(value: "+typ+") -> Int { return 1 }\n")
		command := exec.Command(binary, "tools", entry)
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		if err := command.Run(); err == nil || stdout.Len() != 0 || !strings.Contains(stderr.String(), "type argument") {
			t.Fatalf("expected an empty type argument error for %s: %v, %q, %q", typ, err, &stdout, &stderr)
		}
	}
}
