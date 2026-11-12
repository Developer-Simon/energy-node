//go:build !linux

package media

import "context"

// CheckWifiKey kennt ohne NetworkManager keine Profile.
func CheckWifiKey(ctx context.Context, ssid, password string) string {
	return WifiKeyUnknown
}

// WifiInRange kennt ohne NetworkManager keine Netze.
func WifiInRange(ctx context.Context) []WifiNetwork {
	return nil
}
