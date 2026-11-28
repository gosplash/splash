package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"testing"
)

func rejectedPolicyChain(n int) string {
	var src strings.Builder
	src.WriteString("module bench\nuse std/ai\nredline fn f0() -> Int { return ai.prompt<Int>(\"question\") }\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&src, "redline fn f%d() -> Int { return f%d() }\n", i, i-1)
	}
	fmt.Fprintf(&src, "agent fn entry() -> Int { return f%d() }\n", n-1)
	return src.String()
}

func TestCheckJSONBoundsRejectedChainWitnesses(t *testing.T) {
	const n = 32
	path := writeSplashFile(t, t.TempDir(), "chain.splash", rejectedPolicyChain(n))
	var output bytes.Buffer
	if ok, err := runCheckJSON(path, &output); ok || err != nil {
		t.Fatalf("want a rejected check, got %v, %v", ok, err)
	}

	var report struct {
		Verdict     string
		Diagnostics []struct {
			Code, Message    string
			WitnessLength    int
			WitnessTruncated *bool
			Source           checkTestRange
			Witness          []struct{ Target string }
		}
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Verdict != "rejected" || len(report.Diagnostics) != 2*n+1 {
		t.Fatalf("got verdict %q with %d diagnostics", report.Verdict, len(report.Diagnostics))
	}
	seen := make(map[struct {
		code string
		line int
	}]bool)
	for _, d := range report.Diagnostics {
		line := d.Source.Start.Line
		key := struct {
			code string
			line int
		}{d.Code, line}
		if seen[key] || line < 3 || line > n+3 ||
			(d.Code != "INFERRED_EFFECT_EXCEEDS_AUTHORITY" && (d.Code != "POLICY_VIOLATION" || line == n+3)) {
			t.Fatalf("unexpected or duplicate diagnostic %s at line %d", d.Code, line)
		}
		seen[key] = true
		length := line - 2
		if d.Code == "POLICY_VIOLATION" {
			length = n + 4 - line
		}
		truncated := length > 8
		if d.WitnessLength != length || d.WitnessTruncated == nil || *d.WitnessTruncated != truncated {
			t.Fatalf("%s at line %d: want length=%d, truncated=%v, got length=%d, truncated=%v",
				d.Code, line, length, truncated, d.WitnessLength, d.WitnessTruncated)
		}
		if d.Code == "POLICY_VIOLATION" {
			if line == 3 && !strings.Contains(d.Message, "call path: entry → … (omitted 25 of 33 functions) … → f6 → f5 → f4 → f3 → f2 → f1 → f0") {
				t.Fatalf("wrong redline path: %s", d.Message)
			}
			continue
		}
		if len(d.Witness) != min(length, 8) || d.Witness[len(d.Witness)-1].Target != "std/ai.prompt" {
			t.Fatalf("wrong effect witness at line %d: %+v", line, d.Witness)
		}
		if line == n+3 && (d.Witness[0].Target != "f31" || d.Witness[6].Target != "f25" ||
			!strings.Contains(d.Message, "omitted 25 of 33 steps")) {
			t.Fatalf("wrong effect path: %+v, %s", d.Witness, d.Message)
		}
	}

	var repeat bytes.Buffer
	if ok, err := runCheckJSON(path, &repeat); ok || err != nil || !bytes.Equal(output.Bytes(), repeat.Bytes()) {
		t.Fatalf("repeated check changed the rejection report, got %v, %v", ok, err)
	}
}

func TestCheckOverlappingPolicyRoots(t *testing.T) {
	const roots, layers = 4, 8
	source := overlappingPolicySource(roots, layers)
	path := writeSplashFile(t, t.TempDir(), "roots.splash", source)
	var output bytes.Buffer
	if ok, err := runCheckJSON(path, &output); !ok || err != nil {
		t.Fatalf("expected overlapping roots to verify: %v, %s", err, &output)
	}

	// One sandbox denies the shared authority. Mark a shared callee redline
	// and add a gap outside the closures, where approval still requires coverage.
	source = strings.Replace(source, "deny: [Net]", "deny: [DB.read]", 1)
	source = strings.Replace(source, "fn left0()", "redline fn left0()", 1)
	source += "fn outside(cb: fn() -> Int) -> Int { return cb() }\n"
	path = writeSplashFile(t, t.TempDir(), "roots.splash", source)
	output.Reset()
	if ok, err := runCheckJSON(path, &output); ok || err != nil {
		t.Fatalf("expected overlapping roots to reject: %v, %s", err, &output)
	}
	var report struct {
		Verdict     string
		Diagnostics []checkTestDiagnostic
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	got := make(map[string]int)
	for _, d := range report.Diagnostics {
		got[d.PolicyRuleID]++
	}
	want := map[string]int{
		"sandbox.denied_effect":    3*layers + 3, // diamonds, tail, approval and sandbox root
		"redline.agent_reachable":  1,
		"coverage.required_source": 1,
	}
	if report.Verdict != "rejected" || !maps.Equal(got, want) {
		t.Fatalf("got verdict %q and policy findings %v, want rejected with %v", report.Verdict, got, want)
	}

	var repeat bytes.Buffer
	if ok, err := runCheckJSON(path, &repeat); ok || err != nil || !bytes.Equal(output.Bytes(), repeat.Bytes()) {
		t.Fatalf("repeated check changed overlapping-root report, got %v, %v", ok, err)
	}
}
