package media

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
)

// DebianCDURL is the directory on the Debian CD image server that holds the
// current release. Its SHA512SUMS and SHA512SUMS.sign files are signed by the
// Debian CD signing key.
const DebianCDURL = "https://cdimage.debian.org/debian-cd/current"

// DebianKeyFingerprint is the primary key fingerprint of the Debian CD signing
// key that is embedded in debian_key.asc.
const DebianKeyFingerprint = "DF9B9C49EAA9298432589D76DA87E80D6294BE9B"

// ErrSignature reports that the SHA512SUMS file is not signed by the Debian
// CD key, or that its signature is missing.
var ErrSignature = errors.New("media: debian signature invalid")

// ErrNoRelease reports that SHA512SUMS has no netinst ISO for the requested
// architecture.
var ErrNoRelease = errors.New("media: no debian netinst release for architecture")

// debianKeyData is the Debian CD signing keyring, fetched from
// https://ftp-master.debian.org/keys/ and checked against DebianKeyFingerprint.
//
//go:embed debian_key.asc
var debianKeyData []byte

// DebianRelease describes the netinst ISO that the signed SHA512SUMS lists.
type DebianRelease struct {
	Arch   string // z. B. amd64
	Name   string // z. B. debian-13.7.0-amd64-netinst.iso
	URL    string
	SHA512 string // hex
	Size   int64  // 0 = unknown
}

// DebianKey returns the embedded Debian CD signing keyring.
func DebianKey() (openpgp.EntityList, error) {
	keys, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(debianKeyData))
	if err != nil {
		return nil, fmt.Errorf("read embedded debian key: %w", err)
	}
	return keys, nil
}

// ParseSHA512SUMS verifies the detached signature over signed with keys, then
// returns the netinst ISO for arch from the verified file. The signature may
// be armored or binary. A failed or missing signature returns ErrSignature.
func ParseSHA512SUMS(signed, signature []byte, keys openpgp.EntityList, arch string) (DebianRelease, error) {
	if len(signature) == 0 {
		return DebianRelease{}, fmt.Errorf("%w: signature missing", ErrSignature)
	}
	var err error
	if bytes.HasPrefix(bytes.TrimSpace(signature), []byte("-----BEGIN")) {
		_, err = openpgp.CheckArmoredDetachedSignature(keys, bytes.NewReader(signed), bytes.NewReader(signature), nil)
	} else {
		_, err = openpgp.CheckDetachedSignature(keys, bytes.NewReader(signed), bytes.NewReader(signature), nil)
	}
	if err != nil {
		return DebianRelease{}, fmt.Errorf("%w: %v", ErrSignature, err)
	}

	pattern := regexp.MustCompile(`^debian-\d+(?:\.\d+)*-` + regexp.QuoteMeta(arch) + `-netinst\.iso$`)
	for _, line := range strings.Split(string(signed), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		hash, name := fields[0], strings.TrimPrefix(fields[1], "*")
		if !pattern.MatchString(name) {
			continue
		}
		if len(hash) != 128 {
			return DebianRelease{}, fmt.Errorf("media: debian SHA512 for %s has length %d", name, len(hash))
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return DebianRelease{}, fmt.Errorf("media: debian SHA512 for %s: %w", name, err)
		}
		return DebianRelease{
			Arch:   arch,
			Name:   name,
			URL:    fmt.Sprintf("%s/%s/iso-cd/%s", DebianCDURL, arch, name),
			SHA512: strings.ToLower(hash),
		}, nil
	}
	return DebianRelease{}, ErrNoRelease
}
