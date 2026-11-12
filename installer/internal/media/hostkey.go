package media

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"strings"

	"github.com/GehirnInc/crypt"
	_ "github.com/GehirnInc/crypt/sha512_crypt"
	"golang.org/x/crypto/ssh"
)

// HostKey is the ed25519 SSH host key the installer pins into the image, so
// the device keeps the same identity on first boot.
type HostKey struct {
	PrivatePEM []byte // OpenSSH format
	Public     ssh.PublicKey
	PublicLine string // "ssh-ed25519 AAAA..."
}

// NewHostKey generates a fresh ed25519 host key.
func NewHostKey() (HostKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return HostKey{}, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return HostKey{}, err
	}
	pub, err := ssh.NewPublicKey(priv.Public())
	if err != nil {
		return HostKey{}, err
	}
	return HostKey{
		PrivatePEM: pem.EncodeToMemory(block),
		Public:     pub,
		PublicLine: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))),
	}, nil
}

// HashPassword returns a sha512-crypt ("$6$...") hash of plain with a random salt.
func HashPassword(plain string) (string, error) {
	return crypt.SHA512.New().Generate([]byte(plain), nil)
}
