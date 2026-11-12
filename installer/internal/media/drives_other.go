//go:build !linux

package media

import "context"

// ListDrives liefert ErrUnsupported, weil lsblk nur unter Linux existiert.
func ListDrives(ctx context.Context) ([]Drive, error) {
	return nil, ErrUnsupported
}
