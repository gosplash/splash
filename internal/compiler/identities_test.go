package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentitiesUseCapturedSourcesOnDemand(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "main.splash")
	lib := filepath.Join(root, "lib.splash")
	write := func(path, src string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(src), 0600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) *AnalyzedProgram {
		t.Helper()
		p, diagnostics, err := Analyze(path)
		if err != nil {
			t.Fatalf("analyze: %v, %v", err, diagnostics)
		}
		return p
	}
	write(entry, "module app\nuse lib\nfn main() { run() }\n")
	write(lib, "module lib\nfn run() {}\n")
	original := read(entry)
	if _, _, err := Verify(original); err != nil {
		t.Fatal(err)
	}
	if original.snapshot.identity != (AnalysisIdentity{}) {
		t.Fatal("ordinary verification initialized identity")
	}
	write(lib, "module lib\nfn run() {}\n// changed bytes only\n")
	updated := read(entry).Identity()
	first := original.Identity()
	if first.SourceID == "" || first.AnalysisID == "" || first.SourceID == updated.SourceID || first.AnalysisID == updated.AnalysisID {
		t.Fatalf("captured byte change must affect both digests: %+v, %+v", first, updated)
	}

	write(lib, "module lib\nfn run() {}\n")
	write(filepath.Join(root, "unused.splash"), "unrelated to the closure\n")
	if restored := read(entry).Identity(); restored != first {
		t.Fatalf("expected restored identity: %+v vs %+v", restored, first)
	}
	if again := original.Identity(); again != first {
		t.Fatalf("original identity changed: %+v vs %+v", again, first)
	}
}

func TestAnalysisIdentityBindsEntrySeparatelyFromSource(t *testing.T) {
	sources := map[string]string{"first.splash": "first bytes", "second.splash": "second bytes"}
	first := (&AnalyzedProgram{snapshot: &analysis{entryPath: "first.splash", sources: sources}}).Identity()
	second := (&AnalyzedProgram{snapshot: &analysis{entryPath: "second.splash", sources: sources}}).Identity()
	for _, id := range []AnalysisIdentity{first, second} {
		for _, field := range []struct{ digest, prefix string }{
			{id.SourceID, "splash-source-v1:sha256:"},
			{id.AnalysisID, "splash-analysis-v1:sha256:"},
		} {
			hash, ok := strings.CutPrefix(field.digest, field.prefix)
			if !ok || len(hash) != 64 || strings.Trim(hash, "0123456789abcdef") != "" {
				t.Errorf("unexpected digest format: %q", field.digest)
			}
		}
	}
	if first.SourceID != second.SourceID || first.AnalysisID == second.AnalysisID {
		t.Fatalf("changing the entry should only change the analysis identity: %+v, %+v", first, second)
	}
}

func TestIdentityDistinguishesLexicalEntryCollision(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	child := filepath.Join(dir, "elsewhere", "child")
	for _, sub := range []string{root, child} {
		if err := os.MkdirAll(sub, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(child, filepath.Join(root, "link")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	for path, source := range map[string]string{
		filepath.Join(filepath.Dir(child), "main.splash"): "module entry\nuse main as backing\ntype Item { value: Int }\nfn own(x: Item) {}\n",
		filepath.Join(root, "main.splash"):                "module imported\ntype Item { value: String }\nfn own(x: Item) {}\n",
	} {
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Preserve the literal dot-dot so the OS resolves it after traversing link.
	entry := root + string(filepath.Separator) + filepath.FromSlash("link/../main.splash")
	if bytes, err := os.ReadFile(entry); err != nil || !strings.HasPrefix(string(bytes), "module entry") {
		t.Skipf("filesystem does not traverse dot-dot after this symlink: %v", err)
	}
	var want AnalysisIdentity
	for i := range 10 {
		p, diagnostics, err := Analyze(entry)
		if err != nil {
			t.Fatalf("analyze: %v, %v", err, diagnostics)
		}
		id := p.Identity()
		if i == 0 {
			want = id
		} else if id != want {
			t.Fatalf("identity changed between captures: %+v vs %+v", id, want)
		}
		own, err := p.InspectFunction("entry.own")
		if err != nil {
			t.Fatal(err)
		}
		imported, err := p.InspectFunction("imported.own")
		if err != nil {
			t.Fatal(err)
		}
		if own.Signature.Parameters[0].TypeIdentity.Path == imported.Signature.Parameters[0].TypeIdentity.Path {
			t.Fatal("entry and imported records share a normalized locator")
		}
	}
}
