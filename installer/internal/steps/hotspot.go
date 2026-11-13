package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// DefaultHotspotPath is the emergency hotspot's credentials on the node, written
// once by step 67 (scripts/bootstrap/67-network.sh) as root:energynode 0640.
const DefaultHotspotPath = "/etc/energy-node/hotspot.json"

// ErrHotspotUnreadable means hotspot.json exists but cannot be read or is not
// the expected shape. Its text never carries the file's content.
var ErrHotspotUnreadable = errors.New("the emergency hotspot credentials are unreadable")

// Hotspot is the emergency hotspot's name and password, shown once on the
// result screen so the operator can write them down.
type Hotspot struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
}

// ReadHotspot returns the emergency hotspot's credentials from the node. A
// missing file is no error and no credentials (nil, nil): the node may not
// have a WLAN module, and the result screen then shows no block. The file is
// 0640 root:energynode, so a plain read is tried first and sudo -n covers the
// rest. The password only travels over the SSH connection into memory, never
// into the run log.
func ReadHotspot(ctx context.Context, opts RunOptions) (*Hotspot, error) {
	hotspotPath := opts.HotspotPath
	if hotspotPath == "" {
		hotspotPath = DefaultHotspotPath
	}
	quoted := transport.ShellQuote(hotspotPath)
	command := fmt.Sprintf(`[ -e %[1]s ] || exit 0; cat %[1]s 2>/dev/null || sudo -n cat %[1]s`, quoted)
	var stdout bytes.Buffer
	if err := opts.Client.Run(ctx, command, &stdout, io.Discard); err != nil {
		return nil, ErrHotspotUnreadable
	}
	if stdout.Len() == 0 {
		return nil, nil
	}
	var hotspot Hotspot
	if err := json.Unmarshal(stdout.Bytes(), &hotspot); err != nil || hotspot.SSID == "" || hotspot.Password == "" {
		return nil, ErrHotspotUnreadable
	}
	return &hotspot, nil
}
