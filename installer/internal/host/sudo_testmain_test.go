package host

import (
	"context"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// Most tests hand the host a transport.Client without a connection. Setting up
// sudo would dereference it, so they run with that step switched off. sudo_test.go
// tests the real function directly.
func init() {
	ensureSudoNopasswd = func(context.Context, *transport.Client, string) error { return nil }
}
