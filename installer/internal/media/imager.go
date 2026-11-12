package media

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

// ImagerKind tells whether the Raspberry Pi Imager runs as a native binary or as a Flatpak.
type ImagerKind string

const (
	ImagerNative  ImagerKind = "native"
	ImagerFlatpak ImagerKind = "flatpak"
)

// FlatpakID is the Flatpak application ID of the Raspberry Pi Imager.
const FlatpakID = "org.raspberrypi.rpi-imager"

// Imager is a located Raspberry Pi Imager.
type Imager struct {
	Kind    ImagerKind
	Path    string
	Version string
}

// Env wraps the system calls so tests can replace them.
type Env struct {
	LookPath func(string) (string, error) // exec.LookPath
	Output   func(ctx context.Context, name string, args ...string) ([]byte, error)
}

var imagerVersionRe = regexp.MustCompile(`Raspberry Pi Imager v([0-9][0-9.]*)`)

// FindImager looks for "rpi-imager" in PATH first and falls back to the Flatpak.
// The version is read from the "Raspberry Pi Imager vX.Y.Z" line of "--version".
// ok is false if neither is available.
func FindImager(ctx context.Context, env Env) (im Imager, ok bool) {
	if path, err := env.LookPath("rpi-imager"); err == nil {
		return Imager{Kind: ImagerNative, Path: path, Version: imagerVersion(ctx, env, path, "--version")}, true
	}
	if _, err := env.Output(ctx, "flatpak", "info", FlatpakID); err == nil {
		version := imagerVersion(ctx, env, "flatpak", "run", "--command=rpi-imager", FlatpakID, "--version")
		return Imager{Kind: ImagerFlatpak, Path: "flatpak", Version: version}, true
	}
	return Imager{}, false
}

// imagerVersion runs a version command and returns the version from its output, or "".
func imagerVersion(ctx context.Context, env Env, name string, args ...string) string {
	out, err := env.Output(ctx, name, args...)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if m := imagerVersionRe.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

// TooOld reports whether the imager is older than major version 2, or its version is unknown.
func (im Imager) TooOld() bool {
	major, _, _ := strings.Cut(im.Version, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return true
	}
	return n < 2
}

// WriteArgs describes one imager write run.
type WriteArgs struct {
	Image, Drive                      string
	UserData, NetworkConfig, FirstRun string   // paths, empty = not set
	SystemDrive                       bool     // --enable-writing-system-drives
	ReadDirs                          []string // for Flatpak --filesystem=<dir>:ro
}

// Command returns the program and arguments that write the image with this imager.
// The native binary runs through pkexec, the Flatpak grants read access to ReadDirs
// and gets the device through udisks2.
func (im Imager) Command(a WriteArgs) (name string, args []string) {
	var flags []string
	if a.SystemDrive {
		flags = append(flags, "--enable-writing-system-drives")
	}
	if a.UserData != "" {
		flags = append(flags, "--cloudinit-userdata", a.UserData)
	}
	if a.NetworkConfig != "" {
		flags = append(flags, "--cloudinit-networkconfig", a.NetworkConfig)
	}
	if a.FirstRun != "" {
		flags = append(flags, "--first-run-script", a.FirstRun)
	}

	if im.Kind == ImagerFlatpak {
		args = []string{"run"}
		for _, dir := range a.ReadDirs {
			args = append(args, "--filesystem="+dir+":ro")
		}
		args = append(args, "--command=rpi-imager", FlatpakID, "--cli")
		args = append(args, flags...)
		args = append(args, a.Image, a.Drive)
		return "flatpak", args
	}

	args = []string{im.Path, "--cli"}
	args = append(args, flags...)
	args = append(args, a.Image, a.Drive)
	return "pkexec", args
}
