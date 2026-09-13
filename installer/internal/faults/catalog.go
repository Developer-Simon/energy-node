// Package faults maps the stable fault codes emitted by scripts/bootstrap/
// (Plan A, Plan A-II) and by internal/bundle (Plan B-I, Vertrag 4) to an
// operator-facing message and remediation, in English. Plan C's internal/i18n
// treats this table as its built-in English catalog rather than duplicating
// the text -- see this plan's Architecture section for why the mapping does
// not go through i18n directly.
package faults

import "sort"

// Code is a stable fault code as emitted in a "##STEP <id> fail <code>"
// marker or a bundle.Error. Codes are never renamed once shipped (Plan A-II,
// Global Constraints) so this package can map them without a version check.
type Code string

// Entry is the human-facing translation of one Code: what happened, and what
// to do about it.
type Entry struct {
	Code        Code
	Message     string
	Remediation string
}

var catalog = map[Code]Entry{
	"APT_UPDATE_FAILED": {
		Message:     "`apt update` failed on the node.",
		Remediation: "Check the node's network connection and its /etc/apt/sources.list, then rerun the step.",
	},
	"APT_INSTALL_FAILED": {
		Message:     "`apt install` failed to install one of the required system packages.",
		Remediation: "Rerun with the node connected to the internet; a mirror outage is the most common cause.",
	},
	"MOSQUITTO_CONF_FOREIGN": {
		Message:     "An existing Mosquitto configuration that this installer does not manage was found.",
		Remediation: "Move or remove the conflicting file under /etc/mosquitto/conf.d/ and rerun the step.",
	},
	"MOSQUITTO_PASSWD_FAILED": {
		Message:     "`mosquitto_passwd` failed to write the broker's password file.",
		Remediation: "Check that /etc/mosquitto is writable and that the node has free disk space, then rerun the step.",
	},
	"MOSQUITTO_ARGS_MISSING": {
		Message:     "The Mosquitto setup step was called without the required MQTT username or password file.",
		Remediation: "This is an installer-internal error, not something to fix on the node; report it.",
	},
	"UFW_FAILED": {
		Message:     "The firewall step could not apply its rules with `ufw`.",
		Remediation: "Check that ufw is installed and not held by another process, then rerun the step.",
	},
	"TAILSCALE_TARBALL_MISSING": {
		Message:     "The Tailscale 1.62.0 tarball for this node's architecture was not found in the bundle.",
		Remediation: "Rebuild the bundle; a build for this architecture is missing the tarball under tailscale/.",
	},
	"TAILSCALE_FLAG_INVALID": {
		Message:     "/etc/default/tailscaled sets --tun=userspace-networking, which prevents the node from routing to any other tailnet peer.",
		Remediation: "Remove --tun=userspace-networking from /etc/default/tailscaled and rerun the step (see INSTALLATION.md section 3.1).",
	},
	"TAILSCALE_INSTALL_FAILED": {
		Message:     "Installing the Tailscale package on the node failed.",
		Remediation: "Check the node's disk space and the tarball's integrity, then rerun the step.",
	},
	"BUNDLE_INCOMPLETE": {
		Message:     "The bundle is missing files that the wheel installation step expects.",
		Remediation: "Rebuild the bundle; do not attempt to patch the node directly.",
	},
	"WHEELS_MISSING": {
		Message:     "One or more required Python wheels are missing from the bundle for this node's architecture.",
		Remediation: "Rebuild the bundle for this architecture; a package without a matching piwheels build would have failed the build already (E11), so this points at a stale or partial bundle.",
	},
	"ARCH_MISMATCH": {
		Message:     "The bundle was built for a different CPU architecture than this node reports.",
		Remediation: "Download or build a bundle matching the node's `uname -m` output.",
	},
	"PYTHON_ABI_MISMATCH": {
		Message:     "The bundle's Python wheels were built for a different Python minor version or ABI than the node has installed.",
		Remediation: "Download or build a bundle matching the node's `python3 --version`.",
	},
	"PIP_EXTERNALLY_MANAGED": {
		Message:     "pip refused to install packages because the system marks itself as externally managed (PEP 668).",
		Remediation: "This should not happen: the bootstrap step already passes --break-system-packages. Report this as a bug.",
	},
	"PIP_INSTALL_FAILED": {
		Message:     "`pip install --no-index` failed even though the required wheels are present in the bundle.",
		Remediation: "Check the step's log for the specific package; a corrupted wheel usually means the bundle needs rebuilding.",
	},
	"DASHBOARD_BINARY_MISSING": {
		Message:     "The dashboard binary for this node's architecture is missing from the bundle.",
		Remediation: "Rebuild the bundle; the dashboard cross-compile step for this architecture did not produce a binary.",
	},
	"CONFIG_TEMPLATE_MISSING": {
		Message:     "The bundle does not contain a config.json template.",
		Remediation: "Rebuild the bundle from a checkout that has services/energy-node.config.json.",
	},
	"MANIFESTS_MISSING": {
		Message:     "The bundle is missing one or more service manifest files.",
		Remediation: "Rebuild the bundle; every selected service needs a manifest under config/manifests/.",
	},
	"SUDOERS_INVALID": {
		Message:     "The sudoers fragment for the dashboard's system-action helper failed `visudo -cf`.",
		Remediation: "This points at a broken bundle, not the node's existing sudo configuration (the check runs against the bundled file before it is installed). Rebuild the bundle.",
	},
	"SECRET_FILE_MISSING": {
		Message:     "Neither the MQTT password nor the dashboard admin password was supplied for a fresh install.",
		Remediation: "The dashboard installed but cannot authenticate yet; rerun `installer ensure-secrets` to supply the missing password(s).",
	},
	"DASHBOARD_START_FAILED": {
		Message:     "The dashboard service did not report as active after installation.",
		Remediation: "Check `systemctl status energy-node-dashboard.service` on the node and its journal for the actual error.",
	},
	"CADDY_BINARY_MISSING": {
		Message:     "The Caddy side-package was not found, but HTTPS was selected.",
		Remediation: "Rerun the deploy with the Caddy side-package present, or deselect HTTPS if it was not intended (see E13).",
	},
	"CADDY_CONFIG_INVALID": {
		Message:     "`caddy validate` rejected the generated Caddyfile.",
		Remediation: "This points at a bundle or configuration bug rather than something to edit on the node; report it.",
	},
	"CADDY_START_FAILED": {
		Message:     "The Caddy service did not report as active after installation.",
		Remediation: "Check `systemctl status caddy.service` on the node and its journal for the actual error.",
	},
	"SERVICE_SOURCE_MISSING": {
		Message:     "The bundle is missing the Python source for one of the selected device or automation services.",
		Remediation: "Rebuild the bundle; this service's directory under services/ did not make it into the bundle.",
	},
	"SERVICE_UNIT_FAILED": {
		Message:     "Installing the systemd unit for one of the selected services failed.",
		Remediation: "Check that /etc/systemd/system is writable on the node and that the unit file in the bundle is well-formed.",
	},
	"SERVICE_START_FAILED": {
		Message:     "One of the selected services did not report as active after installation.",
		Remediation: "Check `systemctl status <service>.service` on the node and its journal for the actual error.",
	},
	"BUNDLE_MANIFEST_MISSING": {
		Message:     "manifest.json is missing from the bundle.",
		Remediation: "The bundle is incomplete or was not fully transferred; rebuild or re-download it.",
	},
	"BUNDLE_SIGNATURE_INVALID": {
		Message:     "The bundle's manifest.json.sig does not verify against the embedded signing key, or manifest.json does not match what was signed.",
		Remediation: "Do not proceed with this bundle: rebuild it from a trusted checkout, or re-download it from the original source.",
	},
	"BUNDLE_HASH_MISMATCH": {
		Message:     "A file in the bundle does not match the SHA-256 sum recorded in manifest.json.",
		Remediation: "The bundle is corrupted or was tampered with after signing; rebuild or re-download it.",
	},
}

// Lookup returns the catalog entry for code, with Code filled in on the
// returned Entry, and ok=false for a code this catalog does not know about.
func Lookup(code string) (Entry, bool) {
	entry, ok := catalog[Code(code)]
	if !ok {
		return Entry{}, false
	}
	entry.Code = Code(code)
	return entry, true
}

// Unknown builds a generic entry for a code this catalog has no text for --
// a newer bootstrap script shipped a code this binary predates, for
// instance. Callers use this instead of failing to print anything at all.
func Unknown(code string) Entry {
	return Entry{
		Code:        Code(code),
		Message:     "The step failed with an error code this installer version does not recognise: " + code + ".",
		Remediation: "Check the step's log output above for details, or update the installer to a version that knows this code.",
	}
}

// Codes returns every code this catalog has an entry for, sorted, mainly for
// this package's own completeness test.
func Codes() []Code {
	codes := make([]Code, 0, len(catalog))
	for code := range catalog {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	return codes
}
