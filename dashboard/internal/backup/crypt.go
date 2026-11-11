package backup

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
)

// Format des verschlüsselten Stroms: Magie, Salz, Iterationen, dann Blöcke.
// Jeder Block ist uint32 Länge und AES-256-GCM-Chiffretext mit Tag. Die Nonce
// besteht aus 8 Byte des Salzes und dem uint32-Blockzähler. Das Zusatzdatum
// ist ein Byte: 1 beim letzten Block, sonst 0.
const (
	cryptMagic             = "ENBK1\n"
	cryptSaltLength        = 16
	cryptDefaultIterations = 200000
	cryptMaxIterations     = 10000000
	cryptBlockSize         = 1 << 20
	cryptKeyLength         = 32
	cryptNoncePrefix       = 8
	cryptHeaderLength      = len(cryptMagic) + cryptSaltLength + 4
	cryptFlagMiddle        = 0
	cryptFlagLast          = 1
)

// IsEncrypted meldet, ob head mit der Magie eines verschlüsselten Archivs beginnt.
func IsEncrypted(head []byte) bool {
	return bytes.HasPrefix(head, []byte(cryptMagic))
}

// NewEncryptWriter verschlüsselt alles, was in den Rückgabewert geschrieben wird,
// nach w. Close schreibt den letzten Block, schließt w aber nicht.
func NewEncryptWriter(w io.Writer, password string) (io.WriteCloser, error) {
	if password == "" {
		return nil, ErrPasswordRequired
	}
	salt := make([]byte, cryptSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	aead, err := newCryptAEAD(password, salt, cryptDefaultIterations)
	if err != nil {
		return nil, err
	}
	head := make([]byte, 0, cryptHeaderLength)
	head = append(head, cryptMagic...)
	head = append(head, salt...)
	head = binary.BigEndian.AppendUint32(head, cryptDefaultIterations)
	if _, err := w.Write(head); err != nil {
		return nil, err
	}
	return &encryptWriter{
		w:      w,
		aead:   aead,
		prefix: salt[:cryptNoncePrefix],
		buf:    make([]byte, 0, cryptBlockSize),
	}, nil
}

// NewDecryptReader liest einen Strom aus NewEncryptWriter. Das Passwort wird
// erst am ersten Block geprüft, Read liefert dann ErrWrongPassword.
func NewDecryptReader(r io.Reader, password string) (io.Reader, error) {
	if password == "" {
		return nil, ErrPasswordRequired
	}
	head := make([]byte, cryptHeaderLength)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, fmt.Errorf("%w: Kopf fehlt: %v", ErrCorrupt, err)
	}
	if !IsEncrypted(head) {
		return nil, fmt.Errorf("%w: keine Verschlüsselung erkannt", ErrIncompatible)
	}
	salt := head[len(cryptMagic) : len(cryptMagic)+cryptSaltLength]
	iterations := binary.BigEndian.Uint32(head[len(head)-4:])
	if iterations < 1 || iterations > cryptMaxIterations {
		return nil, fmt.Errorf("%w: Iterationen %d sind nicht erlaubt", ErrCorrupt, iterations)
	}
	aead, err := newCryptAEAD(password, salt, int(iterations))
	if err != nil {
		return nil, err
	}
	return &decryptReader{r: r, aead: aead, prefix: salt[:cryptNoncePrefix]}, nil
}

// newCryptAEAD leitet den Schlüssel ab und baut AES-256-GCM.
func newCryptAEAD(password string, salt []byte, iterations int) (cipher.AEAD, error) {
	key := auth.DeriveKey(password, salt, iterations, cryptKeyLength)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// cryptNonce setzt die Nonce aus dem Präfix und dem Blockzähler zusammen.
func cryptNonce(prefix []byte, counter uint32) []byte {
	nonce := make([]byte, 0, cryptNoncePrefix+4)
	nonce = append(nonce, prefix...)
	return binary.BigEndian.AppendUint32(nonce, counter)
}

// encryptWriter sammelt Klartext in Blöcken und schreibt sie verschlüsselt.
type encryptWriter struct {
	w       io.Writer
	aead    cipher.AEAD
	prefix  []byte
	counter uint32
	buf     []byte
	err     error
	closed  bool
}

func (e *encryptWriter) Write(p []byte) (int, error) {
	if e.closed {
		return 0, errors.New("Verschlüsselung ist schon geschlossen")
	}
	if e.err != nil {
		return 0, e.err
	}
	written := 0
	for len(p) > 0 {
		// Ein voller Block wird erst geschrieben, wenn mehr Daten folgen.
		// So bleibt der letzte Block für Close übrig.
		if len(e.buf) == cryptBlockSize {
			if err := e.flush(cryptFlagMiddle); err != nil {
				return written, err
			}
		}
		n := min(len(p), cryptBlockSize-len(e.buf))
		e.buf = append(e.buf, p[:n]...)
		p = p[n:]
		written += n
	}
	return written, nil
}

// Close schreibt den letzten Block. Ein leerer Strom bekommt einen leeren letzten Block.
func (e *encryptWriter) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	if e.err != nil {
		return e.err
	}
	return e.flush(cryptFlagLast)
}

func (e *encryptWriter) flush(flag byte) error {
	if e.counter == math.MaxUint32 {
		e.err = errors.New("Das Archiv hat zu viele Blöcke")
		return e.err
	}
	sealed := e.aead.Seal(nil, cryptNonce(e.prefix, e.counter), e.buf, []byte{flag})
	frame := binary.BigEndian.AppendUint32(make([]byte, 0, 4+len(sealed)), uint32(len(sealed)))
	frame = append(frame, sealed...)
	if _, err := e.w.Write(frame); err != nil {
		e.err = err
		return err
	}
	e.counter++
	e.buf = e.buf[:0]
	return nil
}

// decryptReader liest Blöcke, prüft sie und gibt den Klartext weiter.
type decryptReader struct {
	r       io.Reader
	aead    cipher.AEAD
	prefix  []byte
	counter uint32
	buf     []byte
	last    bool
	err     error
}

func (d *decryptReader) Read(p []byte) (int, error) {
	for len(d.buf) == 0 {
		if d.err != nil {
			return 0, d.err
		}
		if d.last {
			d.err = d.checkEnd()
			if d.err != nil {
				return 0, d.err
			}
			continue
		}
		if err := d.nextBlock(); err != nil {
			d.err = err
			return 0, err
		}
	}
	n := copy(p, d.buf)
	d.buf = d.buf[n:]
	return n, nil
}

// checkEnd verlangt nach dem letzten Block das Ende des Stroms.
func (d *decryptReader) checkEnd() error {
	var one [1]byte
	n, err := io.ReadFull(d.r, one[:])
	if n > 0 {
		return fmt.Errorf("%w: Daten nach dem letzten Block", ErrCorrupt)
	}
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	return err
}

// nextBlock liest und öffnet den nächsten Block. Ohne Längenfeld oder Inhalt
// ist der Strom abgeschnitten.
func (d *decryptReader) nextBlock() error {
	var lengthBytes [4]byte
	if _, err := io.ReadFull(d.r, lengthBytes[:]); err != nil {
		return truncatedError(err)
	}
	length := binary.BigEndian.Uint32(lengthBytes[:])
	if length < uint32(d.aead.Overhead()) || length > cryptBlockSize+uint32(d.aead.Overhead()) {
		return fmt.Errorf("%w: Blocklänge %d ist nicht erlaubt", ErrCorrupt, length)
	}
	sealed := make([]byte, length)
	if _, err := io.ReadFull(d.r, sealed); err != nil {
		return truncatedError(err)
	}
	nonce := cryptNonce(d.prefix, d.counter)
	plain, err := d.aead.Open(nil, nonce, sealed, []byte{cryptFlagMiddle})
	if err != nil {
		plain, err = d.aead.Open(nil, nonce, sealed, []byte{cryptFlagLast})
		if err != nil {
			if d.counter == 0 {
				return ErrWrongPassword
			}
			return fmt.Errorf("%w: Block %d stimmt nicht", ErrCorrupt, d.counter)
		}
		d.last = true
	}
	d.buf = plain
	d.counter++
	return nil
}

// truncatedError macht aus einem Lesefehler mitten im Strom ein ErrCorrupt.
func truncatedError(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: der Strom ist abgeschnitten", ErrCorrupt)
	}
	return err
}
