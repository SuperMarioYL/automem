package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestVersionLockstep is the single-source-of-truth guard against version
// drift: it asserts that the three in-repo version surfaces the v0.4.0 tag
// shipped out of lockstep — the VERSION file, the `version` var this binary
// reports via `automem --version` (overridable by goreleaser's
// -X main.version ldflag, but the in-source default must track the release) —
// and the head entry of CHANGELOG.md all read the same version.
//
// automem is a Go CLI with no web/site.json (unlike a sibling project whose
// site.json content_version is a fourth axis); the lockstep invariant here is
// therefore VERSION == main.version == CHANGELOG head. The test locates the
// repo-root files via runtime.Caller so it is independent of the test's
// working directory (go test runs from the package dir).
//
// On the v0.4.0 tag this test FAILS: VERSION and main.version read 0.3.0
// while the CHANGELOG head is 0.1.0 (the v0.2.0/v0.3.0/v0.4.0 releases were
// never recorded) — proving the drift is real rather than asserted by eye.
func TestVersionLockstep(t *testing.T) {
	repoRoot, err := repoRootFromCaller()
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	versionFile, err := readFileTrimmed(filepath.Join(repoRoot, "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	changelogHead, err := changelogHeadVersion(filepath.Join(repoRoot, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG head: %v", err)
	}

	// version is the package var main.go bakes in (mirrored by goreleaser's
	// -X main.version at release time). A `go build` from source reports it
	// verbatim, so it must track the VERSION file.
	if version != versionFile {
		t.Errorf("version drift: main.version = %q but VERSION file = %q (a `go build` from source reports the wrong version)", version, versionFile)
	}
	if version != changelogHead {
		t.Errorf("version drift: main.version = %q but CHANGELOG head = %q (the changelog a reader opens is behind the shipped code)", version, changelogHead)
	}
	if versionFile != changelogHead {
		t.Errorf("version drift: VERSION file = %q but CHANGELOG head = %q", versionFile, changelogHead)
	}
}

// repoRootFromCaller resolves the repository root from this test file's
// location (cmd/automem/version_test.go -> repo root three dirs up), so the
// test is robust regardless of the working directory go test uses.
func repoRootFromCaller() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("version: runtime.Caller unavailable")
	}
	// cmd/automem/version_test.go -> repo root is three directories up:
	// version_test.go -> cmd/automem -> cmd -> <repo root>.
	return filepath.Dir(filepath.Dir(filepath.Dir(file))), nil
}

// changelogHeadVersion parses a Keep a Changelog file and returns the version
// of the first released `## [X.Y.Z]` heading — the head of the changelog,
// skipping the `## [Unreleased]` section that sits above it.
func changelogHeadVersion(path string) (string, error) {
	data, err := readFileTrimmed(path)
	if err != nil {
		return "", err
	}
	heading := regexp.MustCompile(`(?m)^##\s*\[([^\]]+)\]`)
	matches := heading.FindAllStringSubmatch(data, -1)
	if len(matches) == 0 {
		return "", errors.New("version: no `## [x.y.z]` heading found in CHANGELOG.md")
	}
	for _, m := range matches {
		ver := strings.TrimSpace(m[1])
		if ver == "Unreleased" {
			continue
		}
		return ver, nil
	}
	return "", errors.New("version: CHANGELOG.md has only an Unreleased section")
}

// readFileTrimmed reads a file and returns its content with surrounding
// whitespace trimmed (the VERSION file and CHANGELOG head comparison should
// not be thrown by a trailing newline).
func readFileTrimmed(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
