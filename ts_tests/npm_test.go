package ts_tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTypeScriptSDK(t *testing.T) {
	runNPMTests(t, filepath.Join("..", "pkg", "sdk", "typescript"))
}

func TestTypeScriptEngine(t *testing.T) {
	runNPMTests(t, filepath.Join("..", "pkg", "engine", "typescript"))
}

func runNPMTests(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directory %s not found: %v", dir, err)
	}
	npmPath, err := exec.LookPath("npm")
	if err != nil {
		t.Skip("npm not found in PATH")
		return
	}
	ensureNodeModules(t, npmPath, dir)
	cmd := exec.Command(npmPath, "test")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("npm test failed in %s: %v\n%s", dir, err, string(out))
	}
	t.Logf("npm test output for %s:\n%s", dir, string(out))
}

func ensureNodeModules(t *testing.T, npmPath, dir string) {
	t.Helper()
	modules := filepath.Join(dir, "node_modules")
	if _, err := os.Stat(modules); err == nil {
		return
	}
	cmd := exec.Command(npmPath, "install", "--no-fund", "--no-audit")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("npm install failed in %s: %v\n%s", dir, err, string(out))
	}
	t.Logf("npm install output for %s:\n%s", dir, string(out))
}
