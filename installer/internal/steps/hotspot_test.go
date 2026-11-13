package steps_test

import (
	"context"
	"errors"
	"path"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

func TestReadHotspotReturnsTheNameAndPassword(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	file := path.Join(t.TempDir(), "hotspot.json")
	if err := client.UploadBytes([]byte(`{"ssid":"EnergyNode-4F2A","password":"sonne-wolke-42"}`), file, 0o640); err != nil {
		t.Fatal(err)
	}
	hotspot, err := steps.ReadHotspot(context.Background(), steps.RunOptions{Client: client, HotspotPath: file})
	if err != nil {
		t.Fatalf("ReadHotspot: %v", err)
	}
	if hotspot == nil || hotspot.SSID != "EnergyNode-4F2A" || hotspot.Password != "sonne-wolke-42" {
		t.Fatalf("hotspot = %+v, want the SSID and password from the file", hotspot)
	}
}

func TestReadHotspotWithoutFileIsNoBlockAndNoError(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	missing := path.Join(t.TempDir(), "missing", "hotspot.json")
	hotspot, err := steps.ReadHotspot(context.Background(), steps.RunOptions{Client: client, HotspotPath: missing})
	if err != nil {
		t.Fatalf("ReadHotspot without a file: %v, want no error", err)
	}
	if hotspot != nil {
		t.Fatalf("hotspot = %+v, want nil without a file", hotspot)
	}
}

func TestReadHotspotReportsABrokenFileWithoutTheContent(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	file := path.Join(t.TempDir(), "hotspot.json")
	if err := client.UploadBytes([]byte(`{"ssid":"EnergyNode-4F2A","password":"sonne-wolke-42"`), file, 0o640); err != nil {
		t.Fatal(err)
	}
	_, err := steps.ReadHotspot(context.Background(), steps.RunOptions{Client: client, HotspotPath: file})
	if !errors.Is(err, steps.ErrHotspotUnreadable) {
		t.Fatalf("err = %v, want ErrHotspotUnreadable", err)
	}
	if strings.Contains(err.Error(), "sonne-wolke-42") {
		t.Fatalf("error leaks the password: %v", err)
	}
}
