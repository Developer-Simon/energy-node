package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestMain macht die Testdatei selbst zum Imager: mit gesetzter Variable endet der Prozess
// mit dem angegebenen Code.
func TestMain(m *testing.M) {
	if code := os.Getenv("ENERGY_NODE_FAKE_IMAGER_EXIT"); code != "" {
		n, err := strconv.Atoi(code)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(125)
		}
		os.Exit(n)
	}
	os.Exit(m.Run())
}

func TestMediaShimPassesTheExitCode(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	imager := filepath.Join(t.TempDir(), "rpi-imager.exe")
	if err := os.WriteFile(imager, data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENERGY_NODE_FAKE_IMAGER_EXIT", "3")

	if got := runMediaShimCmd([]string{"--", imager}); got != 3 {
		t.Fatalf("runMediaShimCmd = %d, want 3", got)
	}
}
