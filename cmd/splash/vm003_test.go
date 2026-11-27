package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/lowering"
	"gosplash.dev/splash/internal/vm"
)

func TestVM003InvoiceOracle(t *testing.T) {
	binary := buildSplashCLI(t)
	emptyPath := t.TempDir()
	fixture := filepath.Join("..", "..", "benchmarks", "ttcs", "invoice")
	var cases []struct {
		Name     string
		Inputs   [3]int
		Expected [3]int
	}
	if err := json.Unmarshal(readVM003Fixture(t, filepath.Join(fixture, "oracle", "cases.json")), &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 9 {
		t.Fatalf("invoice oracle has %d cases, want 9", len(cases))
	}

	var splashDriver, goDriver strings.Builder
	splashDriver.WriteString("module invoice_probe\nuse invoice as candidate\nfn main() {\n")
	goDriver.WriteString("package main\nimport \"fmt\"\nfunc main() {\n")
	for index, test := range cases {
		inputs := fmt.Sprintf("%d, %d, %d", test.Inputs[0], test.Inputs[1], test.Inputs[2])
		fmt.Fprintf(&splashDriver, "    let result_%d = candidate.invoice_totals(%s)\n", index, inputs)
		fmt.Fprintf(&goDriver, "    result%d := InvoiceTotals(%s)\n", index, inputs)
		for _, field := range []string{"subtotal", "discount", "due"} {
			fmt.Fprintf(&splashDriver, "    println(result_%d.%s)\n", index, field)
		}
		for _, field := range []string{"Subtotal", "Discount", "Due"} {
			fmt.Fprintf(&goDriver, "    fmt.Println(result%d.%s)\n", index, field)
		}
	}
	splashDriver.WriteString("}\n")
	goDriver.WriteString("}\n")

	assertOracle := func(t *testing.T, output string, wantMismatch string) {
		t.Helper()
		lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
		if len(lines) != 3*len(cases) {
			t.Fatalf("got %d result fields, want %d. Output %q", len(lines), 3*len(cases), output)
		}
		for index, test := range cases {
			got := lines[3*index : 3*index+3]
			want := []string{strconv.Itoa(test.Expected[0]), strconv.Itoa(test.Expected[1]), strconv.Itoa(test.Expected[2])}
			if !slices.Equal(got, want) {
				if test.Name != wantMismatch {
					t.Fatalf("%s: got %v, want %v", test.Name, got, want)
				}
				if truncating := []string{"1", "0", "1"}; !slices.Equal(got, truncating) {
					t.Fatalf("%s: got %v, want truncating result %v", test.Name, got, truncating)
				}
				return
			}
		}
		if wantMismatch != "" {
			t.Fatalf("oracle accepted truncating starter, want rejection on %q", wantMismatch)
		}
	}

	for _, test := range []struct {
		name         string
		lane         string
		wantMismatch string
	}{
		{name: "reference", lane: "oracle"},
		{name: "truncating starter", lane: "task", wantMismatch: "half-cent rounds up"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			splashSource := readVM003Fixture(t, filepath.Join(fixture, test.lane, "testdata", "splash", "invoice.splash"))
			writeSplashFile(t, dir, "invoice.splash", string(splashSource))
			program, err := analyzeProgram(writeSplashFile(t, dir, "probe.splash", splashDriver.String()))
			if err != nil {
				t.Fatal(err)
			}
			t.Run("VM", func(t *testing.T) {
				assertOracle(t, runVM003(t, program), test.wantMismatch)
			})
			t.Run("VM CLI without Go", func(t *testing.T) {
				command := exec.Command(binary, "run", "probe.splash", "--backend", "vm")
				command.Dir = dir
				command.Env = append(os.Environ(), "PATH="+emptyPath)
				output, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("run VM CLI: %v\n%s", err, output)
				}
				assertOracle(t, string(output), test.wantMismatch)
			})
			t.Run("Go backend", func(t *testing.T) {
				assertOracle(t, runVM003GeneratedGo(t, program), test.wantMismatch)
			})
			t.Run("native Go fixture", func(t *testing.T) {
				goSource := readVM003Fixture(t, filepath.Join(fixture, test.lane, "testdata", "go", "invoice.go"))
				invoice := writeSplashFile(t, dir, "invoice.go", string(goSource))
				probe := writeSplashFile(t, dir, "probe.go", goDriver.String())
				output, err := exec.Command("go", "run", invoice, probe).CombinedOutput()
				if err != nil {
					t.Fatalf("run native Go invoice: %v\n%s", err, output)
				}
				assertOracle(t, string(output), test.wantMismatch)
			})
		})
	}
}

func TestVM003RecordInitializersEvaluateInSourceOrder(t *testing.T) {
	path := writeSplashFile(t, t.TempDir(), "record.splash", `module record_order

type Entry {
    count: Int
    label: String
    enabled: Bool
}

fn make_count() -> Int {
    println("count")
    return 42
}

fn make_label() -> String {
    println("label")
    return "fixture"
}

fn make_enabled() -> Bool {
    println("enabled")
    return true
}

fn main() {
    let entry = Entry { enabled: make_enabled(), count: make_count(), label: make_label() }
    println(entry.count)
    println(entry.label)
    println(entry.enabled)
}
`)
	program, err := analyzeProgram(path)
	if err != nil {
		t.Fatal(err)
	}
	const want = "enabled\ncount\nlabel\n42\nfixture\ntrue\n"
	t.Run("VM", func(t *testing.T) {
		if got := runVM003(t, program); got != want {
			t.Fatalf("VM printed %q, want %q", got, want)
		}
	})
	t.Run("Go backend", func(t *testing.T) {
		if got := runVM003GeneratedGo(t, program); got != want {
			t.Fatalf("Go printed %q, want %q", got, want)
		}
	})
}

func TestVM003ResolvesNominalRecordsAndCallsAcrossImports(t *testing.T) {
	dir := t.TempDir()
	writeSplashFile(t, dir, "numbers.splash", `module numbers
expose Result, make, read

type Result {
    value: Int
}

fn helper(value: Int) -> Int {
    return value + 2
}

fn make(value: Int) -> Result {
    return Result { value: helper(value) }
}

fn read(result: Result) -> Int {
    return result.value
}
`)
	writeSplashFile(t, dir, "labels.splash", `module labels
expose Result, make, read

type Result {
    value: String
}

fn helper(value: String) -> String {
    return value
}

fn make(value: String) -> Result {
    return Result { value: helper(value) }
}

fn read(result: Result) -> String {
    return result.value
}
`)
	path := writeSplashFile(t, dir, "main.splash", `module main
use numbers as numeric
use labels as textual

fn helper() -> Int {
    return 100
}

fn main() {
    let number = numeric.make(40)
    let label = textual.make("invoice")
    println(numeric.read(number))
    println(textual.read(label))
    println(numeric.read(numeric.Result { value: 9 }))
    println(textual.read(textual.Result { value: "qualified" }))
    println(helper())
}
`)
	program, err := analyzeProgram(path)
	if err != nil {
		t.Fatal(err)
	}
	const want = "42\ninvoice\n9\nqualified\n100\n"
	if got := runVM003(t, program); got != want {
		t.Fatalf("VM printed %q, want %q", got, want)
	}
}

func TestVM003RejectsUnsupportedRecordFieldsBeforeOutput(t *testing.T) {
	for _, test := range []struct {
		name  string
		field string
	}{
		{name: "optional", field: "value: Int?"},
		{name: "default", field: "value: Int = 4"},
		{name: "annotated", field: "@sensitive value: Int"},
		{name: "nested", field: "value: Inner"},
		{name: "unexpected type arguments", field: "value: Int<Bool>"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "module main\ntype Inner { value: Int }\ntype Outer { " + test.field +
				" }\nfn main() {\n    println(\"first\")\n}\n"
			path := writeSplashFile(t, t.TempDir(), "record.splash", source)
			program, err := analyzeProgram(path)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			err = program.runVM(&output, vm.Limits{Instructions: 1000, Frames: 8})
			var admission *lowering.AdmissionError
			if !errors.As(err, &admission) || admission.Source.File != path || admission.Source.Line != 3 {
				t.Fatalf("want unsupported record field diagnostic at %s:3, got %v", path, err)
			}
			if output.Len() != 0 {
				t.Fatalf("unsupported input produced output %q", output.String())
			}
		})
	}
}

func TestVM003RejectsTypeArgumentsOnMonomorphicTypes(t *testing.T) {
	for _, source := range []string{
		"fn print_value(x: Int<Bool>) { println(x) }\nfn main() { print_value(1) }",
		"fn value() -> Int<Bool> { return 1 }\nfn main() { println(value()) }",
		"fn main() {\nlet value: Int<Bool> = 1\nprintln(value)\n}",
	} {
		path := writeSplashFile(t, t.TempDir(), "annotations.splash", "module main\n"+source+"\n")
		program, err := analyzeProgram(path)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		var admission *lowering.AdmissionError
		err = program.runVM(&output, vm.Limits{Instructions: 1000, Frames: 8})
		if !errors.As(err, &admission) || admission.Source.File != path || output.Len() != 0 {
			t.Fatalf("want admission error before execution for %q, got %v and output %q", source, err, output.String())
		}
	}
}

func readVM003Fixture(t *testing.T, path string) []byte {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func runVM003(t *testing.T, program *analyzedProgram) string {
	t.Helper()
	var output bytes.Buffer
	if err := program.runVM(&output, vm.Limits{Instructions: 1000, Frames: 8}); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func runVM003GeneratedGo(t *testing.T, program *analyzedProgram) string {
	t.Helper()
	source, err := program.emitGo()
	if err != nil {
		t.Fatal(err)
	}
	// Match the package normalization done by splash build for executable output.
	if module := program.merged.Module; module != nil && module.Name != "main" {
		source = strings.Replace(source, "package "+module.Name+"\n", "package main\n", 1)
	}
	generated := writeSplashFile(t, t.TempDir(), "generated.go", source)
	output, err := exec.Command("go", "run", generated).CombinedOutput()
	if err != nil {
		t.Fatalf("run generated Go: %v\n%s", err, output)
	}
	return string(output)
}
