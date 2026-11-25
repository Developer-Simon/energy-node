package media

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeEnv answers LookPath and Output from fixed tables. Output keys are "name arg1 arg2 ...".
type fakeEnv struct {
	paths   map[string]string
	outputs map[string]string
}

func (f fakeEnv) env() Env {
	return Env{
		LookPath: func(name string) (string, error) {
			if p, ok := f.paths[name]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		},
		Output: func(_ context.Context, name string, args ...string) ([]byte, error) {
			key := strings.Join(append([]string{name}, args...), " ")
			if out, ok := f.outputs[key]; ok {
				return []byte(out), nil
			}
			return nil, errors.New("no such command: " + key)
		},
	}
}

func TestFindImagerPrefersTheNativeBinary(t *testing.T) {
	f := fakeEnv{
		paths:   map[string]string{"rpi-imager": "/usr/bin/rpi-imager"},
		outputs: map[string]string{"/usr/bin/rpi-imager --version": "Raspberry Pi Imager v2.0.12\n"},
	}
	im, ok := FindImager(context.Background(), f.env())
	if !ok {
		t.Fatal("imager not found")
	}
	if im.Kind != ImagerNative || im.Version != "2.0.12" || im.Path != "/usr/bin/rpi-imager" {
		t.Fatalf("got %+v", im)
	}
}

func TestFindImagerFallsBackToFlatpak(t *testing.T) {
	f := fakeEnv{
		outputs: map[string]string{
			"flatpak info org.raspberrypi.rpi-imager":                               "Ref: app/org.raspberrypi.rpi-imager\n",
			"flatpak run --command=rpi-imager org.raspberrypi.rpi-imager --version": "Starting background drive list polling\nRaspberry Pi Imager v2.0.12\n",
		},
	}
	im, ok := FindImager(context.Background(), f.env())
	if !ok {
		t.Fatal("imager not found")
	}
	if im.Kind != ImagerFlatpak || im.Version != "2.0.12" {
		t.Fatalf("got %+v", im)
	}
}

func TestFindImagerReportsMissing(t *testing.T) {
	_, ok := FindImager(context.Background(), fakeEnv{}.env())
	if ok {
		t.Fatal("expected ok == false")
	}
}

func TestTooOld(t *testing.T) {
	cases := map[string]bool{"1.8.5": true, "2.0.0": false, "": true}
	for version, want := range cases {
		if got := (Imager{Version: version}).TooOld(); got != want {
			t.Errorf("TooOld(%q) = %v, want %v", version, got, want)
		}
	}
}

func TestFlatpakCommandGrantsTheReadDirs(t *testing.T) {
	im := Imager{Kind: ImagerFlatpak, Path: "flatpak", Version: "2.0.12"}
	name, args := im.Command(WriteArgs{
		Image:    "/c/img/os.img.xz",
		Drive:    "/dev/sdb",
		UserData: "/c/job/user-data",
		ReadDirs: []string{"/c/img", "/c/job"},
	})
	want := []string{
		"run",
		"--filesystem=/c/img:ro",
		"--filesystem=/c/job:ro",
		"--command=rpi-imager",
		FlatpakID,
		"--cli",
		"--cloudinit-userdata", "/c/job/user-data",
		"/c/img/os.img.xz",
		"/dev/sdb",
	}
	if name != "flatpak" {
		t.Errorf("name = %q, want flatpak", name)
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v\nwant   %v", args, want)
	}
}

func TestNativeCommandRunsThroughPkexec(t *testing.T) {
	im := Imager{Kind: ImagerNative, Path: "/usr/bin/rpi-imager", Version: "2.0.12"}
	name, args := im.Command(WriteArgs{
		Image:       "/c/img/os.img.xz",
		Drive:       "/dev/sdb",
		SystemDrive: true,
		FirstRun:    "/c/job/firstrun.sh",
	})
	if name != "pkexec" {
		t.Fatalf("name = %q, want pkexec", name)
	}
	if args[0] != im.Path {
		t.Errorf("args[0] = %q, want %q", args[0], im.Path)
	}
	idxSystem, idxFirstRun := indexOf(args, "--enable-writing-system-drives"), indexOf(args, "--first-run-script")
	if idxSystem < 0 || idxFirstRun < 0 || idxSystem > idxFirstRun {
		t.Errorf("flags out of order: %v", args)
	}
	if n := len(args); args[n-2] != "/c/img/os.img.xz" || args[n-1] != "/dev/sdb" {
		t.Errorf("last two args = %v, want image and drive", args[n-2:])
	}
	if indexOf(args, "--disable-eject") >= 0 {
		t.Errorf("--disable-eject must not be passed: %v", args)
	}
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

func TestWindowsImagerCommandRunsTheExeDirectly(t *testing.T) {
	im := Imager{Kind: ImagerWindows, Path: `C:\P\rpi-imager.exe`}
	name, args := im.Command(WriteArgs{
		Image:       `C:\c\img\os.img.xz`,
		Drive:       `\\.\PhysicalDrive1`,
		UserData:    `C:\c\job\user-data`,
		SystemDrive: true,
	})
	want := []string{"--cli", "--enable-writing-system-drives", "--cloudinit-userdata", `C:\c\job\user-data`, `C:\c\img\os.img.xz`, `\\.\PhysicalDrive1`}
	if name != `C:\P\rpi-imager.exe` {
		t.Errorf("name = %q, want the exe path", name)
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v\nwant   %v", args, want)
	}
}

// fakeWinEnv answers the Windows lookups from fixed tables.
func fakeWinEnv(vars map[string]string, existing []string, version string, versionErr error) WinEnv {
	exists := map[string]bool{}
	for _, p := range existing {
		exists[p] = true
	}
	return WinEnv{
		Getenv: func(name string) string { return vars[name] },
		Exists: func(path string) bool { return exists[path] },
		FileVersion: func(string) (string, error) {
			return version, versionErr
		},
	}
}

var winVars = map[string]string{
	"ProgramFiles":      `C:\Program Files`,
	"ProgramFiles(x86)": `C:\Program Files (x86)`,
	"LOCALAPPDATA":      `C:\Users\simon\AppData\Local`,
}

func TestFindWindowsImagerPrefersProgramFiles(t *testing.T) {
	env := fakeWinEnv(winVars, []string{
		`C:\Program Files\Raspberry Pi Ltd\Imager\rpi-imager.exe`,
		`C:\Program Files\Raspberry Pi Imager\rpi-imager.exe`,
		`C:\Users\simon\AppData\Local\Programs\Raspberry Pi Imager\rpi-imager.exe`,
	}, "2.0.12.0", nil)
	im, ok := FindWindowsImager(env)
	if !ok {
		t.Fatal("imager not found")
	}
	want := Imager{Kind: ImagerWindows, Path: `C:\Program Files\Raspberry Pi Ltd\Imager\rpi-imager.exe`, Version: "2.0.12"}
	if im != want {
		t.Fatalf("got %+v, want %+v", im, want)
	}
}

func TestFindWindowsImagerFallsBackToLocalAppData(t *testing.T) {
	env := fakeWinEnv(winVars, []string{
		`C:\Users\simon\AppData\Local\Programs\Raspberry Pi Imager\rpi-imager.exe`,
	}, "2.0.12.0", nil)
	im, ok := FindWindowsImager(env)
	if !ok {
		t.Fatal("imager not found")
	}
	if im.Path != `C:\Users\simon\AppData\Local\Programs\Raspberry Pi Imager\rpi-imager.exe` || im.Version != "2.0.12" {
		t.Fatalf("got %+v", im)
	}
}

func TestFindWindowsImagerTrimsTheFourthVersionPart(t *testing.T) {
	env := fakeWinEnv(winVars, []string{
		`C:\Program Files\Raspberry Pi Imager\rpi-imager.exe`,
	}, "2.0.8.0", nil)
	im, ok := FindWindowsImager(env)
	if !ok {
		t.Fatal("imager not found")
	}
	if im.Version != "2.0.8" {
		t.Fatalf("Version = %q, want 2.0.8", im.Version)
	}
}

func TestFindWindowsImagerWithoutVersionIsTooOld(t *testing.T) {
	env := fakeWinEnv(winVars, []string{
		`C:\Program Files\Raspberry Pi Imager\rpi-imager.exe`,
	}, "", errors.New("no version info"))
	im, ok := FindWindowsImager(env)
	if !ok {
		t.Fatal("imager not found")
	}
	if im.Version != "" || !im.TooOld() {
		t.Fatalf("got %+v, want empty version that is too old", im)
	}
}

func TestFindWindowsImagerNotInstalled(t *testing.T) {
	if _, ok := FindWindowsImager(fakeWinEnv(winVars, nil, "", nil)); ok {
		t.Fatal("expected ok == false")
	}
}
