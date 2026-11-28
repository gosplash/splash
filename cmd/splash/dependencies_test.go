package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

type testDependencyFunction struct {
	Symbol      string
	Declaration checkTestPoint
}

type testDependencyReport struct {
	SchemaVersion    int
	Query, Direction string
	Result           *struct {
		Root          testDependencyFunction
		MaxDepth      *int
		Relationships []struct {
			Function, Via testDependencyFunction
			Distance      int
			Source        checkTestRange
		}
		Coverage struct {
			Scope, Status string
			Gap           *struct {
				Function testDependencyFunction
				Reason   string
				Source   checkTestRange
			}
		}
	}
	Diagnostics []checkTestDiagnostic
}

func TestDependencyQueries(t *testing.T) {
	bin := buildSplashCLI(t)
	dir := t.TempDir()
	left := writeSplashFile(t, dir, "left.splash", "module left\nfn finish() {}\nfn bridge() { finish() }\n")
	writeSplashFile(t, dir, "right.splash", "module right\nfn finish() {}\n")
	entry := writeSplashFile(t, dir, "main.splash", `module app
use left as alpha
use right as beta
fn root() {
    alpha.bridge()
    beta.finish()
    beta.finish()
}
fn recur() { recur() }
fn gap(cb: fn() -> Int) needs Net -> Int { return cb() }
`)

	direct, raw := readDependencies(t, bin, "callees", entry, "root")
	if _, next := readDependencies(t, bin, "callees", entry, "root"); !bytes.Equal(raw, next) {
		t.Fatalf("nondeterministic JSON:\n%s\n%s", raw, next)
	}
	if direct.Result == nil || direct.Result.Root.Symbol != "app.root" || direct.Result.MaxDepth == nil ||
		*direct.Result.MaxDepth != 1 || direct.Result.Coverage.Status != "complete" ||
		direct.Result.Coverage.Scope != "expandedFunctions" {
		t.Fatalf("unexpected direct result: %s", raw)
	}
	d := direct.Result.Relationships
	if len(d) != 2 || d[0].Function.Symbol != "left.bridge" || d[0].Function.Declaration.File != left ||
		d[0].Distance != 1 || d[0].Via != direct.Result.Root ||
		d[1].Function.Symbol != "right.finish" || d[1].Source.Start.Line != 6 || d[1].Source.End == nil {
		t.Fatalf("wrong direct relationships: %+v", d)
	}

	transitive, raw := readDependencies(t, bin, "callees", entry, "root", "--transitive")
	tr := transitive.Result.Relationships
	if transitive.Result.MaxDepth != nil || len(tr) != 3 || tr[2].Function.Symbol != "left.finish" ||
		tr[2].Distance != 2 || tr[2].Via.Symbol != "left.bridge" || tr[2].Source.Start.File != left {
		t.Fatalf("wrong transitive witness: %s", raw)
	}

	incoming, raw := readDependencies(t, bin, "callers", entry, "left.finish", "--depth", "2")
	r := incoming.Result.Relationships
	if len(r) != 2 || r[0].Function.Symbol != "left.bridge" || r[0].Via.Symbol != "left.finish" ||
		r[1].Function.Symbol != "app.root" || r[1].Via.Symbol != "left.bridge" || r[1].Distance != 2 ||
		incoming.Result.Coverage.Status != "incomplete" || incoming.Result.Coverage.Scope != "entryClosure" ||
		incoming.Result.Coverage.Gap == nil || incoming.Result.Coverage.Gap.Function.Symbol != "app.gap" ||
		incoming.Result.Coverage.Gap.Source.End == nil {
		t.Fatalf("wrong caller witnesses or global gap: %s", raw)
	}

	cycle, raw := readDependencies(t, bin, "callees", entry, "recur", "--transitive")
	if len(cycle.Result.Relationships) != 1 || cycle.Result.Relationships[0].Function != cycle.Result.Root ||
		cycle.Result.Relationships[0].Distance != 1 {
		t.Fatalf("wrong direct self recursion: %s", raw)
	}
	outboundGap, raw := readDependencies(t, bin, "callees", entry, "gap")
	if outboundGap.Result.Coverage.Status != "incomplete" || outboundGap.Result.Coverage.Gap == nil ||
		!reflect.DeepEqual(outboundGap.Result.Relationships, direct.Result.Relationships[:0]) {
		t.Fatalf("expected a known empty result and a local gap: %s", raw)
	}
}

func readDependencies(t *testing.T, bin, direction, path, query string, options ...string) (testDependencyReport, []byte) {
	t.Helper()
	command := exec.Command(bin, append([]string{direction, path, query, "--json"}, options...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || stdout.Len() == 0 {
			t.Fatalf("dependency command failed: %v, stdout %q, stderr %q", err, &stdout, &stderr)
		}
	}
	var report testDependencyReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || stderr.Len() != 0 ||
		report.SchemaVersion != 1 || report.Query != query || report.Direction != direction || report.Diagnostics == nil {
		t.Fatalf("invalid dependency report %q: %v, stderr %q", &stdout, err, &stderr)
	}
	return report, stdout.Bytes()
}

func TestDependencyQueryCoverageAndTies(t *testing.T) {
	bin := buildSplashCLI(t)
	dir := t.TempDir()
	path := writeSplashFile(t, dir, "main.splash", `module app
fn root() {
    zebra()
    alpha()
}
fn alpha() { finish() }
fn zebra() { finish() }
fn finish() { root() }
fn hidden(cb: fn() -> Int) -> Int { return cb() }
fn with_hidden(cb: fn() -> Int) -> Int { return hidden(cb) }
approve fn approve_only() {}
`)
	report, raw := readDependencies(t, bin, "callees", path, "root", "--transitive")
	r := report.Result.Relationships
	if len(r) != 4 || r[2].Function.Symbol != "app.finish" || r[2].Via.Symbol != "app.zebra" ||
		r[2].Distance != 2 || r[3].Function != report.Result.Root || r[3].Distance != 3 {
		t.Fatalf("wrong equally short path tie or cycle: %s", raw)
	}
	direct, raw := readDependencies(t, bin, "callees", path, "with_hidden")
	if direct.Result.Coverage.Status != "complete" || len(direct.Result.Relationships) != 1 {
		t.Fatalf("depth-one coverage should describe the expanded root: %s", raw)
	}
	transitive, raw := readDependencies(t, bin, "callees", path, "with_hidden", "--depth", "2")
	coverage := transitive.Result.Coverage
	if coverage.Status != "incomplete" || coverage.Gap == nil || coverage.Gap.Function.Symbol != "app.hidden" {
		t.Fatalf("missing deeper coverage gap: %s", raw)
	}
	approval, raw := readDependencies(t, bin, "callees", path, "approve_only")
	if approval.Result.Coverage.Status != "complete" || len(approval.Result.Relationships) != 0 {
		t.Fatalf("injected approval host cannot be a Splash declaration: %s", raw)
	}
}

func TestDependencyQueryFailures(t *testing.T) {
	bin := buildSplashCLI(t)
	dir := t.TempDir()
	writeSplashFile(t, dir, "x.splash", "module same\nfn target() {}\n")
	writeSplashFile(t, dir, "y.splash", "module same\nfn target() {}\n")
	entry := writeSplashFile(t, dir, "main.splash", "module main\nuse x as xlib\nuse y as ylib\nfn main() {}\n")
	for _, tc := range []struct{ query, code string }{{"same.target", "FUNCTION_AMBIGUOUS"}, {"unknown", "FUNCTION_NOT_FOUND"}} {
		report, raw := readDependencies(t, bin, "callers", entry, tc.query)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || string(fields["result"]) != "null" ||
			len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != tc.code {
			t.Fatalf("wrong failure for %s: %s", tc.query, raw)
		}
	}
	for _, args := range [][]string{{"--depth", "0"}, {"--depth", "-2"}, {"--transitive", "--depth", "2"}} {
		command := exec.Command(bin, append([]string{"callers", entry, "main", "--json"}, args...)...)
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("accepted invalid options %v: %v, %s, %s", args, err, &stdout, &stderr)
		}
	}
}
