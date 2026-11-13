package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// Resolver looks up host names. net.Resolver satisfies it.
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// NameCheck is the result of CheckName for a proposed host name.
type NameCheck struct {
	Valid      bool   `json:"valid"`
	Taken      bool   `json:"taken"`
	Suggestion string `json:"suggestion"`
}

// Reachability is the result of Probe for a device on the network.
type Reachability struct {
	Reachable  bool `json:"reachable"`
	KeyMatches bool `json:"key_matches"`
}

const (
	nameLookupTimeout = 1500 * time.Millisecond
	probeTimeout      = 3 * time.Second
)

// CheckName reports whether name is a usable host name and whether "<name>.local"
// already resolves on the network. When it does, Suggestion is the first free
// name among <name>-2 to <name>-9, or empty if all of them are taken.
func CheckName(ctx context.Context, r Resolver, name string) NameCheck {
	if !ValidHostname(name) {
		return NameCheck{}
	}
	res := NameCheck{Valid: true, Taken: resolves(ctx, r, name)}
	if !res.Taken {
		return res
	}
	for i := 2; i <= 9; i++ {
		candidate := fmt.Sprintf("%s-%d", name, i)
		if !resolves(ctx, r, candidate) {
			res.Suggestion = candidate
			break
		}
	}
	return res
}

// resolves reports whether "<name>.local" has an address within nameLookupTimeout.
func resolves(ctx context.Context, r Resolver, name string) bool {
	lookupCtx, cancel := context.WithTimeout(ctx, nameLookupTimeout)
	defer cancel()
	addrs, err := r.LookupHost(lookupCtx, name+".local")
	return err == nil && len(addrs) > 0
}

// errProbeDone aborts the handshake once the host key has been seen, because
// no login is attempted.
var errProbeDone = errors.New("probe: host key received")

// Probe connects to host over TCP and starts an SSH handshake without logging
// in. Without a port, host is probed on port 22. Reachable is true when the
// server offered a host key, and KeyMatches compares that key with want.
func Probe(ctx context.Context, host string, want ssh.PublicKey) Reachability {
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "22")
	}
	dialer := net.Dialer{Timeout: probeTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return Reachability{}
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(probeTimeout)); err != nil {
		return Reachability{}
	}

	var seen ssh.PublicKey
	config := &ssh.ClientConfig{
		User: "probe",
		// A server with several host keys signs with the first type both sides
		// list, and Go prefers RSA and ECDSA over ed25519. Ask for the type of
		// the pinned key, or the probe sees another key of the same device.
		HostKeyAlgorithms: hostKeyAlgorithms(want),
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			seen = key
			return errProbeDone
		},
	}
	_, _, _, _ = ssh.NewClientConn(conn, host, config)
	if seen == nil {
		return Reachability{}
	}
	return Reachability{
		Reachable:  true,
		KeyMatches: want != nil && bytes.Equal(seen.Marshal(), want.Marshal()),
	}
}

// hostKeyAlgorithms lists the signature algorithms to request for want. An
// RSA key can be signed as rsa-sha2-256 or rsa-sha2-512. Without a pinned key
// the client default applies.
func hostKeyAlgorithms(want ssh.PublicKey) []string {
	if want == nil {
		return nil
	}
	if want.Type() == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
	}
	return []string{want.Type()}
}
