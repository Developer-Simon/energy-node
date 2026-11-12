//go:build !linux

package media

import "context"

// CheckWifiKey kennt ohne NetworkManager keine Profile.
func CheckWifiKey(ctx context.Context, ssid, password string) string {
	return WifiKeyUnknown
}
