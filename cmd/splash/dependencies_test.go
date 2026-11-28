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
	SchemaVersion               int
	EntryPath, Query, Direction string
	Result                      *struct {
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
		d[1].Function.Symbol != "right.finish" || d[1].Source.Start.Line != 6 || d[1].Source.Start.Column != 16 ||
		d[1].Source.End == nil || d[1].Source.End.Column != 18 {
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
	exitCode := 0
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			t.Fatalf("dependency command failed: %v, stdout %q, stderr %q", err, &stdout, &stderr)
		}
		exitCode = exit.ExitCode()
	}
	var report testDependencyReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || stderr.Len() != 0 ||
		report.SchemaVersion != 1 || report.EntryPath != path ||
		report.Query != query || report.Direction != direction || report.Diagnostics == nil {
		t.Fatalf("invalid dependency report %q: %v, stderr %q", &stdout, err, &stderr)
	}
	wantExit := 0
	if report.Result == nil {
		wantExit = 1
	}
	if exitCode != wantExit {
		t.Fatalf("dependency report exited %d, want %d: %s", exitCode, wantExit, &stdout)
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
	for _, args := range [][]string{
		{"--depth", "0"}, {"--depth", "-2"}, {"--transitive", "--depth", "2"},
		{"--limit", "2"}, {"--compact", "--limit"}, {"--compact", "--limit", "0"},
		{"--compact", "--limit", "101"}, {"--compact", "--limit", "2", "--limit", "3"},
		{"--compact", "--transitive", "--depth", "2"}, {"--compact", "--compact"},
	} {
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

func TestCompactDependenciesKeepCoverageBeyondLimit(t *testing.T) {
	bin := buildSplashCLI(t)
	dir := t.TempDir()
	path := writeSplashFile(t, dir, "app.splash", `module app
fn root(cb: fn() -> Int) -> Int { return middle(cb) }
fn middle(cb: fn() -> Int) -> Int { return last(cb) }
fn last(cb: fn() -> Int) -> Int { return cb() }
`)
	full, fullRaw := readDependencies(t, bin, "callees", path, "root", "--transitive")
	args := []string{"callees", path, "root", "--json", "--compact", "--limit", "1", "--transitive"}
	raw, err := exec.Command(bin, args...).Output()
	if err != nil {
		t.Fatalf("compact dependencies: %v, %s", err, raw)
	}
	var report struct {
		View     string
		Identity struct{ AnalysisID string }
		Result   *struct {
			Root    struct{ Symbol, Path string }
			Matches []struct {
				Symbol, Path   string
				Line, Distance int
			}
			Total, Omitted, Limit int
			OmittedFields         []string
			Coverage              struct {
				Scope, Status string
				Gap           struct{ Function testDependencyFunction }
			}
		}
	}
	if err := json.Unmarshal(raw, &report); err != nil || report.View != "compact" ||
		report.Identity.AnalysisID == "" || report.Result == nil {
		t.Fatalf("invalid compact dependency report: %s (%v)", raw, err)
	}
	got := report.Result
	if got.Root.Symbol != "app.root" || got.Root.Path != "app.splash" ||
		got.Total != len(full.Result.Relationships) || got.Total != 2 || got.Omitted != 1 ||
		got.Limit != 1 || !reflect.DeepEqual(got.OmittedFields, []string{"via", "source"}) ||
		len(got.Matches) != 1 || got.Matches[0].Symbol != full.Result.Relationships[0].Function.Symbol ||
		got.Matches[0].Path != "app.splash" || got.Matches[0].Line != 3 || got.Matches[0].Distance != 1 ||
		got.Coverage.Scope != full.Result.Coverage.Scope || got.Coverage.Status != "incomplete" ||
		got.Coverage.Gap.Function.Symbol != "app.last" {
		t.Fatalf("wrong compact result: %s", raw)
	}
	if next, err := exec.Command(bin, args...).Output(); err != nil || !bytes.Equal(raw, next) {
		t.Fatalf("nondeterministic compact output: %s (%v)", next, err)
	}
	if _, after := readDependencies(t, bin, "callees", path, "root", "--transitive"); !bytes.Equal(fullRaw, after) {
		t.Fatalf("full report changed: %s", after)
	}
}

func TestCompactDependenciesKeepSameNamedDeclarationsDistinct(t *testing.T) {
	bin := buildSplashCLI(t)
	dir := t.TempDir()
	writeSplashFile(t, dir, "left.splash", "module shared\nfn target() {}")
	writeSplashFile(t, dir, "right.splash", "module shared\nfn target() {}")
	path := writeSplashFile(t, dir, "main.splash", "module app\nuse left as left_mod\nuse right as right_mod\nfn main() { left_mod.target(); right_mod.target() }")
	raw, err := exec.Command(bin, "callees", path, "main", "--json", "--compact").Output()
	if err != nil {
		t.Fatalf("compact dependencies: %v, %s", err, raw)
	}
	var report struct {
		Result struct {
			Matches []struct{ Symbol, Path string }
			Limit   int
			Omitted int
		}
	}
	if err := json.Unmarshal(raw, &report); err != nil || report.Result.Limit != 20 || report.Result.Omitted != 0 ||
		len(report.Result.Matches) != 2 || report.Result.Matches[0].Symbol != "shared.target" ||
		report.Result.Matches[1].Symbol != "shared.target" || report.Result.Matches[0].Path != "left.splash" ||
		report.Result.Matches[1].Path != "right.splash" {
		t.Fatalf("nominal dependency locations lost: %s (%v)", raw, err)
	}
}
