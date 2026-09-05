// Package mqttbridge renders a settings.BridgeConnection into the Mosquitto
// bridge directive text that internal/httpapi stages for the root helper to
// install as /etc/mosquitto/conf.d/bridge.conf. Rendering never accepts
// free-form text from the browser - only the typed settings.BridgeConnection
// model, already validated by internal/settings on every load/save path.
// See knowhow/dashboard/dashboard-mqtt-setup.md, Teil B, "Rendering statt
// Freitext".
package mqttbridge

import (
	"fmt"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// PasswordPlaceholder replaces the real remote_password in a masked
// (preview) render - it must never be mistaken for a real password by
// someone reading the UI preview or a revision diff.
const PasswordPlaceholder = "********"

// Input is what Render needs to produce a bridge.conf. Password is the
// remote broker password, kept out of settings.BridgeConnection entirely
// (see that type's doc comment) so it is supplied here explicitly. Mask, if
// true, replaces a non-empty Password with PasswordPlaceholder - used for
// the UI preview and revision display, never for the staged file that is
// actually installed.
type Input struct {
	Connection settings.BridgeConnection
	Password   string
	Mask       bool
	RenderedBy string
	RenderedAt time.Time
}

// startType maps the boolean StartTypeAuto onto the two Mosquitto
// start_type values this dashboard exposes. "lazy" only opens the bridge
// connection once a locally matching topic is published, which is a
// reasonable manual alternative to "automatic" without needing a third
// UI option for "once".
func startType(auto bool) string {
	if auto {
		return "automatic"
	}
	return "lazy"
}

// sanitizeCommentValue defends the header comment against control
// characters in a value that ultimately comes from internal/auth (the
// acting username) rather than from the validated BridgeConnection model.
// Even if this were bypassed, an injected line without a leading '#' would
// still be rejected by the root helper's directive allowlist - see
// knowhow/dashboard/dashboard-mqtt-setup.md, Teil B, "Ablauf beim
// Anwenden".
func sanitizeCommentValue(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Render produces the full bridge.conf content for one connection. It
// re-validates the connection as defense in depth (see
// settings.ValidateBridgeConnection's doc comment) even though every path
// that can produce a settings.BridgeConnection already validated it.
func Render(input Input) (string, error) {
	if err := settings.ValidateBridgeConnection(input.Connection); err != nil {
		return "", err
	}
	connection := input.Connection

	var b strings.Builder
	b.WriteString("# =============================================================================\n")
	b.WriteString("# Erzeugt vom Energy-Node-Dashboard - Aenderungen an dieser Datei werden beim\n")
	b.WriteString("# naechsten Anwenden ueberschrieben.\n")
	fmt.Fprintf(&b, "# Zeitstempel: %s\n", input.RenderedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "# Benutzer: %s\n", sanitizeCommentValue(input.RenderedBy))
	b.WriteString("# =============================================================================\n\n")

	fmt.Fprintf(&b, "connection %s\n\n", connection.Name)
	fmt.Fprintf(&b, "address %s:%d\n", connection.Address, connection.Port)
	if connection.RemoteUsername != "" {
		fmt.Fprintf(&b, "remote_username %s\n", connection.RemoteUsername)
	}
	if input.Password != "" {
		password := input.Password
		if input.Mask {
			password = PasswordPlaceholder
		}
		fmt.Fprintf(&b, "remote_password %s\n", password)
	}
	fmt.Fprintf(&b, "remote_clientid %s\n\n", connection.RemoteClientID)

	for _, topic := range connection.Topics {
		fmt.Fprintf(&b, "topic %s %s %d\n", topic.Pattern, topic.Direction, topic.QoS)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "try_private %t\n", connection.TryPrivate)
	fmt.Fprintf(&b, "start_type %s\n", startType(connection.StartTypeAuto))
	fmt.Fprintf(&b, "restart_timeout %d\n", connection.RestartTimeout)
	fmt.Fprintf(&b, "keepalive_interval %d\n", connection.KeepaliveSeconds)
	fmt.Fprintf(&b, "cleansession %t\n", connection.CleanSession)

	return b.String(), nil
}

// DirectiveChecksum reduces rendered bridge.conf content to just its
// directive lines (no comments, no blank lines) before hashing, so the
// drift check in GET /api/v1/mqtt/bridge/status can compare a freshly
// rendered configuration against the installed file without the header's
// timestamp/username comment always making them differ.
func DirectiveChecksum(content string) string {
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, trimmed)
	}
	return strings.Join(lines, "\n")
}
