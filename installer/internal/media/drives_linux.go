//go:build linux

package media

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const lsblkFields = "NAME,PATH,SIZE,MODEL,TRAN,RM,HOTPLUG,TYPE,FSTYPE,LABEL,FSUSED,MOUNTPOINTS"

// ListDrives ruft lsblk (Linux) bzw. liefert ErrUnsupported.
func ListDrives(ctx context.Context) ([]Drive, error) {
	out, err := runLsblk(ctx, lsblkFields)
	if err != nil {
		// Older lsblk versions do not know MOUNTPOINTS.
		out, err = runLsblk(ctx, strings.Replace(lsblkFields, "MOUNTPOINTS", "MOUNTPOINT", 1))
		if err != nil {
			return nil, err
		}
	}
	return ParseLsblk(out)
}

func runLsblk(ctx context.Context, fields string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "lsblk", "-J", "-b", "-o", fields).Output()
	if err != nil {
		return nil, fmt.Errorf("media: lsblk: %w", err)
	}
	return out, nil
}
