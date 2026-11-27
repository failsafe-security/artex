package englishguard

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel, text string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckFlagsUnlistedChinese(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "server/bad.go", "package server\nvar s = \"中文\"\n")

	violations, err := Check(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 {
		t.Fatalf("violations=%d, want 1: %+v", len(violations), violations)
	}
	if violations[0].Path != "server/bad.go" || violations[0].Line != 2 {
		t.Fatalf("unexpected violation: %+v", violations[0])
	}
}

func TestCheckAllowsListedFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "server/legacy.go", "package server\nvar s = \"中文\"\n")

	violations, err := Check(root, map[string]bool{"server/legacy.go": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("allowlisted file flagged: %+v", violations)
	}
}

func TestCheckSkipsTestsGeneratedAndVendoredFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "server/x_test.go", "package server\nvar s = \"中文\"\n")
	writeFile(t, root, "web/src/lib/chat.test.mjs", "const s = \"中文\";\n")
	writeFile(t, root, "docs/note.md", "中文\n")
	writeFile(t, root, "web/out/app.js", "const s = \"中文\";\n")
	writeFile(t, root, "node_modules/pkg/index.js", "const s = \"中文\";\n")
	writeFile(t, root, "server/webui/dist/app.js", "const s = \"中文\";\n")
	writeFile(t, root, "vendor/lib.go", "package lib\nvar s = \"中文\"\n")

	violations, err := Check(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("skipped files flagged: %+v", violations)
	}
}

func TestLoadAllowlistIgnoresCommentsAndBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allowlist.txt")
	if err := os.WriteFile(path, []byte("\n# comment\nserver/a.go  # reason\n  \nserver/b.ts\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	allowed, err := LoadAllowlist(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"server/a.go": true, "server/b.ts": true}
	if len(allowed) != len(want) {
		t.Fatalf("allowlist=%v, want %v", allowed, want)
	}
	for key := range want {
		if !allowed[key] {
			t.Fatalf("missing %q in %v", key, allowed)
		}
	}
}

func TestMissingAllowlistEntries(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "server/present.go", "package server\n")

	missing := MissingAllowlistEntries(root, map[string]bool{
		"server/present.go": true,
		"server/gone.go":    true,
	})
	if len(missing) != 1 || missing[0] != "server/gone.go" {
		t.Fatalf("missing=%v", missing)
	}
}

func TestFindRepoRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.test\n")
	writeFile(t, root, "a/b/c/file.go", "package c\n")

	got, err := FindRepoRoot(filepath.Join(root, "a", "b", "c"))
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("root=%q, want %q", got, root)
	}
}

// TestRepositorySourceIsEnglish is the guard that runs with the normal Go test
// suite (including CI's isolated per-test runner). It fails when someone adds a
// Chinese string to a shipped source file that is not on the functional allowlist.
func TestRepositorySourceIsEnglish(t *testing.T) {
	root, err := FindRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := LoadAllowlist(filepath.Join(root, "ci", "english_allowlist.txt"))
	if err != nil {
		t.Fatalf("load allowlist: %v", err)
	}

	violations, err := Check(root, allowed)
	if err != nil {
		t.Fatalf("scan repository: %v", err)
	}
	if len(violations) > 0 {
		var b strings.Builder
		b.WriteString("Chinese text found in shipped source (translate it or add a justified allowlist entry):\n")
		for i, v := range violations {
			if i == 20 {
				b.WriteString("... (truncated)\n")
				break
			}
			b.WriteString(v.Path)
			if v.Line > 0 {
				b.WriteString(": " + strconv.Itoa(v.Line))
			}
			b.WriteString(": " + v.Text + "\n")
		}
		t.Fatal(b.String())
	}

	for _, rel := range MissingAllowlistEntries(root, allowed) {
		t.Logf("warning: stale allowlist entry (file not found): %s", rel)
	}
}
