package storage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// testPaths lays out a runtime symlink, a mount with its runtime directory and
// both fallbacks in a temp directory. Only the mount exists up front, as on a real node.
func testPaths(t *testing.T) Paths {
	t.Helper()
	root := t.TempDir()
	p := Paths{
		Runtime:        filepath.Join(root, "data", "runtime"),
		Mount:          filepath.Join(root, "mnt", "energy-node-data"),
		SystemFallback: filepath.Join(root, "data", "runtime-fallback"),
		RAMFallback:    filepath.Join(root, "run", "energy-node", "runtime"),
	}
	if err := os.MkdirAll(DriveRuntimeDir(p), 0755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSwitchIsAtomic(t *testing.T) {
	p := testPaths(t)
	if err := Switch(p, ModeFallbackSystem); err != nil {
		t.Fatalf("initial Switch: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var gaps []string
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := os.Readlink(p.Runtime); err != nil {
				mu.Lock()
				gaps = append(gaps, err.Error())
				mu.Unlock()
			}
		}
	}()

	targets := []Mode{ModeDrive, ModeFallbackRAM, ModeFallbackSystem}
	var switchErr error
	for i := 0; i < 300 && switchErr == nil; i++ {
		switchErr = Switch(p, targets[i%len(targets)])
	}
	close(stop)
	wg.Wait()

	if switchErr != nil {
		t.Fatalf("Switch: %v", switchErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(gaps) > 0 {
		t.Fatalf("runtime symlink missing %d times during switching, first: %s", len(gaps), gaps[0])
	}
}

func TestSwitchCreatesFallbackDir(t *testing.T) {
	p := testPaths(t)

	if err := Switch(p, ModeFallbackSystem); err != nil {
		t.Fatalf("Switch to system fallback: %v", err)
	}
	if fi, err := os.Stat(p.SystemFallback); err != nil || !fi.IsDir() {
		t.Fatalf("system fallback dir not created: %v", err)
	}

	if err := Switch(p, ModeFallbackRAM); err != nil {
		t.Fatalf("Switch to RAM fallback: %v", err)
	}
	if fi, err := os.Stat(p.RAMFallback); err != nil || !fi.IsDir() {
		t.Fatalf("RAM fallback dir not created: %v", err)
	}

	target, err := os.Readlink(p.Runtime)
	if err != nil {
		t.Fatalf("runtime is not a symlink: %v", err)
	}
	if target != p.RAMFallback {
		t.Fatalf("runtime points to %q, want %q", target, p.RAMFallback)
	}
}
