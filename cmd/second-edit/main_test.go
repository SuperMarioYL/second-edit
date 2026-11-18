package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperMarioYL/second-edit/internal/engine/godot"
)

// TestJSONStdoutIsPure locks the agent surface: with --json, stdout carries
// exactly one JSON object and every human line goes to stderr.
func TestJSONStdoutIsPure(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"hero.png", "helmet.png", "hero.tscn"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "hero", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c, cerr := godot.Init(filepath.Join(dir, "hero.png"), filepath.Join(dir, "hero.tscn"))
	if cerr != nil {
		t.Fatal(cerr)
	}
	contractPath := filepath.Join(dir, "hero.asset.json")
	if cerr := c.Save(contractPath); cerr != nil {
		t.Fatal(cerr)
	}

	stdout, stderr, code := captureRun(
		"apply", "--asset", contractPath,
		"--region", "helmet",
		"--patch", filepath.Join(dir, "helmet.png"),
		"--out", filepath.Join(dir, "hero.v2.png"),
		"--json",
	)
	if code != 0 {
		t.Fatalf("apply --json exited %d: %s", code, stderr)
	}
	assertSingleJSON(t, "apply --json", stdout)
	if !strings.Contains(stderr, "wrote hero.v2.png") {
		t.Fatalf("the human lines must be on stderr, got %q", stderr)
	}

	stdout, stderr, code = captureRun("check", "--asset", filepath.Join(dir, "hero.v2.asset.json"), "--json")
	if code != 0 {
		t.Fatalf("check --json exited %d: %s", code, stderr)
	}
	assertSingleJSON(t, "check --json", stdout)
	if !strings.Contains(stderr, "check PASS") || !strings.Contains(stderr, "receipt chain ok") {
		t.Fatalf("the human check lines must be on stderr, got %q", stderr)
	}
}

func captureRun(args ...string) (stdout, stderr string, code int) {
	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()

	code = run(args)
	outW.Close()
	errW.Close()
	outData, _ := io.ReadAll(outR)
	errData, _ := io.ReadAll(errR)
	outR.Close()
	errR.Close()
	return string(outData), string(errData), code
}

func assertSingleJSON(t *testing.T, label, s string) {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		t.Fatalf("%s stdout is not one JSON object: %v\nstdout: %q", label, err, s)
	}
	if _, ok := obj["verdict"]; !ok {
		t.Fatalf("%s JSON has no verdict: %q", label, s)
	}
}
