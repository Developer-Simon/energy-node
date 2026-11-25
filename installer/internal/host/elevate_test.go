package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
	"unsafe"

	"github.com/Developer-Simon/energy-node-installer/internal/media"
)

func TestShellExecuteInfoLayout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("SHELLEXECUTEINFOW ist nur auf 64 bit festgelegt")
	}
	var info shellExecuteInfo
	if got := unsafe.Sizeof(info); got != 112 {
		t.Errorf("Sizeof = %d, want 112", got)
	}
	checks := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"lpVerb", unsafe.Offsetof(info.lpVerb), 16},
		{"nShow", unsafe.Offsetof(info.nShow), 48},
		{"dwHotKey", unsafe.Offsetof(info.dwHotKey), 88},
		{"hProcess", unsafe.Offsetof(info.hProcess), 104},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("Offsetof(%s) = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestHelperArgsPutTheImagerAfterTheSeparator(t *testing.T) {
	f := media.HelperFiles{Log: "/job/imager.log", Cancel: "/job/cancel", Status: "/job/status"}
	got := helperArgs(f, `C:\job\rpi-imager.exe`, []string{"--foo", "bar"})
	want := []string{
		media.HelperName,
		"--log", "/job/imager.log",
		"--cancel", "/job/cancel",
		"--status", "/job/status",
		"--", `C:\job\rpi-imager.exe`, "--foo", "bar",
	}
	if !slices.Equal(got, want) {
		t.Errorf("helperArgs = %q, want %q", got, want)
	}
}

func TestRunElevatedWritesTheCancelFileOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	launch := func(exe string, args []string) (func() (int, error), error) {
		return func() (int, error) {
			<-release
			return 0, nil
		}, nil
	}

	out, wait, err := runElevated(ctx, launch, "installer", dir, "rpi-imager", nil)
	if err != nil {
		t.Fatalf("runElevated: %v", err)
	}
	cancel()

	cancelFile := filepath.Join(dir, "cancel")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(cancelFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancel file was not written after ctx was cancelled")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	if _, err := wait(); err != nil {
		t.Errorf("wait: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestElevatedRunnerMapsCancelledToRightsDenied(t *testing.T) {
	launchErr := fmt.Errorf("uac: %w", media.ErrRightsDenied)
	launch := func(exe string, args []string) (func() (int, error), error) {
		return nil, launchErr
	}

	_, _, err := runElevated(context.Background(), launch, "installer", t.TempDir(), "rpi-imager", nil)
	if err != launchErr {
		t.Fatalf("runElevated err = %v, want the launcher error unchanged", err)
	}
	if !errors.Is(err, media.ErrRightsDenied) {
		t.Errorf("errors.Is(err, ErrRightsDenied) = false for %v", err)
	}
}

func TestRunElevatedWritesTheCancelFileWhileTheLauncherStillBlocks(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seen := false
	launch := func(exe string, args []string) (func() (int, error), error) {
		cancel()
		cancelFile := filepath.Join(dir, "cancel")
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(cancelFile); err == nil {
				seen = true
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		return func() (int, error) { return 0, nil }, nil
	}

	out, wait, err := runElevated(ctx, launch, "installer", dir, "rpi-imager", nil)
	if err != nil {
		t.Fatalf("runElevated: %v", err)
	}
	if !seen {
		t.Error("cancel file did not appear while the launcher was still blocked")
	}
	if _, err := wait(); err != nil {
		t.Errorf("wait: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
