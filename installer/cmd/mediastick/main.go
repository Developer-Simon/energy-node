// cmd/mediastick is a test CLI for the Debian stick. It patches an original
// Debian netinst ISO with the same BuildDebianFiles and isopatch.Patch calls
// the installer makes, but with fixed settings. It is not linked into the
// installer binary.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/media"
	"github.com/Developer-Simon/energy-node-installer/internal/media/isopatch"
)

// Labels as used by the installer in internal/media/job.go.
const (
	label      = "ENERGY-NODE-LOESCHT"
	jolietName = "LÖSCHT PLATTE"
)

func main() {
	in := flag.String("in", "", "original Debian netinst ISO (required)")
	out := flag.String("out", "", "patched stick image to write (required)")
	arch := flag.String("arch", "amd64", "amd64 or arm64")
	ask := flag.Bool("ask", false, "ask before erasing the disk")
	lang := flag.String("lang", "en", "installer language: de or en (amd64 only)")
	pubkey := flag.String("pubkey", "", "installer public key file, one authorized_keys line (required)")
	hostkey := flag.String("hostkey", "", "host key private file in OpenSSH format, public part read from <file>.pub (required)")
	flag.Parse()

	if err := run(*in, *out, *arch, *ask, *lang, *pubkey, *hostkey); err != nil {
		fmt.Fprintln(os.Stderr, "mediastick:", err)
		os.Exit(1)
	}
}

func run(in, out, arch string, ask bool, lang, pubkey, hostkey string) error {
	if in == "" || out == "" || pubkey == "" || hostkey == "" {
		return fmt.Errorf("-in, -out, -pubkey and -hostkey are required")
	}
	authKey, err := readLine(pubkey)
	if err != nil {
		return err
	}
	priv, err := os.ReadFile(hostkey)
	if err != nil {
		return err
	}
	pub, err := readLine(hostkey + ".pub")
	if err != nil {
		return err
	}
	hk := media.HostKey{PrivatePEM: priv, PublicLine: pub}

	ds := media.DebianSettings{
		Settings: media.Settings{
			Hostname:      "energy-node",
			User:          "node",
			AuthorizedKey: authKey,
			Timezone:      "Europe/Berlin",
			Keyboard:      "de",
		},
		Arch:           arch,
		AskBeforeErase: ask,
		InstallLang:    lang,
		SystemLang:     "de",
	}
	files, err := media.BuildDebianFiles(ds, hk)
	if err != nil {
		return err
	}
	return isopatch.Patch(in, out, isopatch.Options{
		Label:       label,
		JolietLabel: jolietName,
		Add:         files.Add,
		Replace:     files.Replace,
	})
}

// readLine returns the first line of a file without surrounding space.
func readLine(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	line, _, _ := bytes.Cut(b, []byte("\n"))
	s := strings.TrimSpace(string(line))
	if s == "" {
		return "", fmt.Errorf("%s: empty", path)
	}
	return s, nil
}
