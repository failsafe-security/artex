// Package englishguard enforces the English-only rule for shipped ARTEX source.
//
// The console, API errors, agent prompts and tool descriptions are English. The
// translation in #19 fixed the drift reported in #15, but nothing stopped a new
// Chinese error string or UI label from landing later. This package provides the
// check used by its repository-wide test.
//
// Chinese is still allowed in a small, explicit set of files where the text is
// functional rather than product copy: legacy markers written by older builds,
// provider quota and ICP strings that match Chinese content, backwards-compatible
// input aliases, and deliberate matchers for Chinese target-site login copy. Those
// paths live in ci/english_allowlist.txt with a reason on each line.
package englishguard

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sourceExtensions are the shipped source types that must be English. Markdown,
// JSON fixtures and Python tooling are not user-facing product copy and are not
// scanned.
var sourceExtensions = map[string]bool{
	".go":   true,
	".ts":   true,
	".tsx":  true,
	".js":   true,
	".mjs":  true,
	".html": true,
}

var skipDirNames = map[string]bool{
	".git":         true,
	".next":        true,
	"artifacts":    true,
	"build":        true,
	"data":         true,
	"dist":         true,
	"node_modules": true,
	"vendor":       true,
}

var skipFileSuffixes = []string{
	"_test.go",
	".test.ts",
	".test.tsx",
	".test.js",
	".test.mjs",
}

var skipPathPrefixes = []string{
	"web/out/",
	"server/webui/dist/",
}

// Violation is one Chinese line in a shipped source file.
type Violation struct {
	Path string
	Line int
	Text string
}

// LoadAllowlist reads repository-relative paths that may contain functional
// Chinese. Blank lines and everything after '#' are ignored.
func LoadAllowlist(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	allowed := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		entry := scanner.Text()
		if i := strings.IndexByte(entry, '#'); i >= 0 {
			entry = entry[:i]
		}
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		allowed[filepath.ToSlash(entry)] = true
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return allowed, nil
}

// Check walks root and returns every line containing a CJK ideograph in a
// shipped source file that is not allowlisted. It is deterministic: files are
// visited in lexical order.
func Check(root string, allowed map[string]bool) ([]Violation, error) {
	root = filepath.Clean(root)
	var violations []Violation

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			if skipDirNames[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !sourceExtensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		for _, prefix := range skipPathPrefixes {
			if strings.HasPrefix(rel, prefix) {
				return nil
			}
		}
		for _, suffix := range skipFileSuffixes {
			if strings.HasSuffix(rel, suffix) {
				return nil
			}
		}
		if allowed[rel] {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			violations = append(violations, Violation{Path: rel, Text: "<unreadable: " + err.Error() + ">"})
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if containsCJK(line) {
				violations = append(violations, Violation{
					Path: rel,
					Line: i + 1,
					Text: strings.TrimSpace(line),
				})
			}
		}
		return nil
	})
	return violations, err
}

// MissingAllowlistEntries returns allowlisted files that no longer exist. They
// are reported (not failed) so the list stays easy to prune as legacy code is
// removed.
func MissingAllowlistEntries(root string, allowed map[string]bool) []string {
	var missing []string
	for rel := range allowed {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			missing = append(missing, rel)
		}
	}
	sort.Strings(missing)
	return missing
}

// FindRepoRoot walks up from start until it finds the go.mod of this module.
// Tests run with their working directory set to the package directory, so the
// repository-wide check needs this to find the tree it should scan.
func FindRepoRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %s", start)
		}
		dir = parent
	}
}

func containsCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}
