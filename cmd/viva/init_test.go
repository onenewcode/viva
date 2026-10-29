package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A project made by viva init must build and pass its own test, or the first thing
// a new user (or a coding assistant) does fails.
func TestInitMakesAProjectThatBuildsAndTests(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a project")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "my-app")
	var output bytes.Buffer
	if err := runInit([]string{"-viva", root, "-tidy=false", directory}, &output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.go", "main_test.go", "viva.toml", "go.mod", "AGENTS.md"} {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil || len(data) == 0 {
			t.Fatalf("%s was not written: %v", name, err)
		}
		if strings.Contains(string(data), "{{") {
			t.Errorf("%s still has a placeholder", name)
		}
	}
	// The project resolves Viva's own dependencies from its go.sum.
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "go.sum"), sum, 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if combined, err := command.CombinedOutput(); err != nil {
		t.Fatalf("the new project's test failed: %v\n%s", err, combined)
	}

	if err := runInit([]string{"-viva", root, "-tidy=false", directory}, &output); err == nil {
		t.Error("init overwrote a project that was already there")
	}
}

// Someone who installed viva with go install has no checkout to replace. init
// must still produce a project, and that project must take Viva from the
// module path instead of a replace into a directory that does not exist.
func TestInitOutsideACheckoutUsesTheModulePath(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "my-app")
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	if err := runInit([]string{"-tidy=false", directory}, &output); err != nil {
		t.Fatalf("init failed outside a checkout: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	goMod := string(data)
	if strings.Contains(goMod, "replace") {
		t.Errorf("go.mod replaces a path that does not exist:\n%s", goMod)
	}
	if !strings.Contains(goMod, "module my-app") {
		t.Errorf("go.mod lost the module line:\n%s", goMod)
	}
	if !strings.Contains(goMod, "tool github.com/vivaui/viva/cmd/viva") {
		t.Errorf("go.mod does not pin the viva command, so go tool viva would not work:\n%s", goMod)
	}
	main, err := os.ReadFile(filepath.Join(directory, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(main), `"github.com/vivaui/viva/app"`) {
		t.Error("main.go does not import the module path")
	}
}
