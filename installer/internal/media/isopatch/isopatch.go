// Package isopatch edits a Debian hybrid netinst ISO in place. It adds files
// to the root directory and replaces small existing files, reusing the
// sectors of a donor file. The image size, partition tables, El Torito
// records and signed binaries stay byte-identical.
//
// Only ISO 9660 with Rock Ridge and Joliet is supported, as shipped by the
// Debian netinst images. Every other layout is refused with ErrLayout.
package isopatch

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"unicode/utf16"
)

const sec = 2048

// ErrLayout marks every structural problem with the image or the request.
var ErrLayout = errors.New("isopatch: unexpected ISO layout")

// File is a new file in the root directory of the image.
type File struct {
	Name string // name in the root directory
	Data []byte
}

// Options describe the changes made by Patch.
type Options struct {
	Label, JolietLabel string            // "" leaves the label unchanged; Joliet at most 16 characters
	Add                []File            // stored in the sectors of Donor
	Replace            map[string][]byte // "/boot/grub/grub.cfg" to new content, must fit the allocated sectors
	Donor              string            // "" means "md5sum.txt"
}

type tree struct {
	joliet  bool
	rootExt uint32
	rootLen uint32
}

type record struct {
	off  int64 // absolute offset in image
	raw  []byte
	name string // RR or Joliet name, without ;1
}

func (r record) extent() uint32 { return binary.LittleEndian.Uint32(r.raw[2:6]) }
func (r record) size() uint32   { return binary.LittleEndian.Uint32(r.raw[10:14]) }
func (r record) isDir() bool    { return r.raw[25]&2 != 0 }

type image struct {
	f     *os.File
	size  int64
	trees []tree
}

func layoutf(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrLayout}, args...)...)
}

// Patch copies in to out and changes out. On any error out is removed.
func Patch(in, out string, o Options) (err error) {
	removeOut := true
	defer func() {
		if err != nil && removeOut {
			_ = os.Remove(out)
		}
	}()

	same, err := sameFile(in, out)
	if err != nil {
		return err
	}
	if same {
		removeOut = false
		return errors.New("isopatch: input and output are the same file")
	}
	if err := validate(o); err != nil {
		return err
	}
	donor := o.Donor
	if donor == "" {
		donor = "md5sum.txt"
	}
	if err := copyFile(in, out); err != nil {
		return err
	}
	im, err := openImage(out)
	if err != nil {
		return err
	}
	defer im.f.Close()

	paths := make([]string, 0, len(o.Replace))
	for p := range o.Replace {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if err := im.replace(p, o.Replace[p]); err != nil {
			return err
		}
	}
	files := make([]newFile, 0, len(o.Add))
	for _, f := range o.Add {
		files = append(files, newFile{name: f.Name, data: f.Data})
	}
	if len(files) > 0 {
		if err := im.donate(donor, files); err != nil {
			return err
		}
	}
	return im.setLabel(o.Label, o.JolietLabel)
}

// sameFile reports whether a and b are the same existing file. A missing b is not an error.
func sameFile(a, b string) (bool, error) {
	ai, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	bi, err := os.Stat(b)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(ai, bi), nil
}

func validate(o Options) error {
	if len(o.Label) > 32 {
		return layoutf("label %q is longer than 32 bytes", o.Label)
	}
	if n := len(utf16.Encode([]rune(o.JolietLabel))); n > 16 {
		return layoutf("joliet label %q is longer than 16 characters", o.JolietLabel)
	}
	for _, f := range o.Add {
		if f.Name == "" || strings.Contains(f.Name, "/") {
			return fmt.Errorf("isopatch: %q is not a file name in the root directory", f.Name)
		}
	}
	return nil
}

func copyFile(in, out string) error {
	src, err := os.Open(in)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(out)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

func openImage(path string) (*image, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	im := &image{f: f, size: fi.Size()}
	if err := im.scan(); err != nil {
		f.Close()
		return nil, err
	}
	return im, nil
}

// scan reads the volume descriptors and records the root of every tree.
func (im *image) scan() error {
	for s := int64(16); s < 64; s++ {
		d, err := im.readAt(s*sec, sec)
		if err != nil {
			return err
		}
		if string(d[1:6]) != "CD001" {
			return layoutf("sector %d has no volume descriptor", s)
		}
		switch d[0] {
		case 255:
			if len(im.trees) == 0 {
				return layoutf("no primary volume descriptor")
			}
			return nil
		case 1, 2:
			if binary.LittleEndian.Uint16(d[128:130]) != sec {
				return layoutf("block size is not 2048")
			}
			if d[0] == 2 && !isJoliet(d) {
				continue // supplementary but not Joliet
			}
			root := d[156:190]
			im.trees = append(im.trees, tree{joliet: d[0] == 2,
				rootExt: binary.LittleEndian.Uint32(root[2:6]),
				rootLen: binary.LittleEndian.Uint32(root[10:14])})
		}
	}
	return layoutf("no volume descriptor terminator")
}

func isJoliet(d []byte) bool {
	return bytes.HasPrefix(d[88:91], []byte("%/"))
}

func (im *image) readAt(off, n int64) ([]byte, error) {
	if off < 0 || n < 0 || off+n > im.size {
		return nil, layoutf("read of %d bytes at offset %d is beyond the image", n, off)
	}
	b := make([]byte, n)
	if _, err := im.f.ReadAt(b, off); err != nil {
		return nil, fmt.Errorf("isopatch: read at offset %d: %w", off, err)
	}
	return b, nil
}

func (im *image) writeAt(off int64, b []byte) error {
	if off < 0 || off+int64(len(b)) > im.size {
		return layoutf("write of %d bytes at offset %d is beyond the image", len(b), off)
	}
	if _, err := im.f.WriteAt(b, off); err != nil {
		return fmt.Errorf("isopatch: write at offset %d: %w", off, err)
	}
	return nil
}

func suOffset(raw []byte) int {
	nl := int(raw[32])
	o := 33 + nl
	if nl%2 == 0 {
		o++
	}
	return o
}

// susp walks the SUSP entries of a record's system use area.
func susp(raw []byte, fn func(sig string, e []byte)) error {
	su := raw[suOffset(raw):]
	for len(su) >= 4 {
		l := int(su[2])
		if l < 4 || l > len(su) {
			break
		}
		sig := string(su[:2])
		if sig == "CE" {
			return layoutf("continuation area (CE) entries are not supported")
		}
		fn(sig, su[:l])
		su = su[l:]
	}
	return nil
}

func recName(raw []byte, joliet bool) (string, error) {
	nl := int(raw[32])
	id := raw[33 : 33+nl]
	if nl == 1 && (id[0] == 0 || id[0] == 1) {
		return string([]byte{'.', '.'}[:1+int(id[0])]), nil
	}
	if joliet {
		u := make([]uint16, nl/2)
		for i := range u {
			u[i] = binary.BigEndian.Uint16(id[2*i:])
		}
		return strings.TrimSuffix(string(utf16.Decode(u)), ";1"), nil
	}
	name := ""
	if err := susp(raw, func(sig string, e []byte) {
		if sig == "NM" && len(e) >= 5 {
			name += string(e[5:])
		}
	}); err != nil {
		return "", err
	}
	if name != "" {
		return name, nil
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(string(id), ";1"), ".")), nil
}

func (im *image) readDir(t tree, ext, size uint32) ([]record, error) {
	var out []record
	base := int64(ext) * sec
	data, err := im.readAt(base, int64(size))
	if err != nil {
		return nil, err
	}
	for p := 0; p < len(data); {
		l := int(data[p])
		if l == 0 {
			p = (p/sec + 1) * sec
			continue
		}
		if l < 34 || p+l > len(data) {
			return nil, layoutf("directory record at offset %d is malformed", base+int64(p))
		}
		raw := append([]byte(nil), data[p:p+l]...)
		if suOffset(raw) > l {
			return nil, layoutf("directory record at offset %d is malformed", base+int64(p))
		}
		name, err := recName(raw, t.joliet)
		if err != nil {
			return nil, err
		}
		out = append(out, record{off: base + int64(p), raw: raw, name: name})
		p += l
	}
	return out, nil
}

func (im *image) lookup(t tree, path string) (record, error) {
	ext, size := t.rootExt, t.rootLen
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		recs, err := im.readDir(t, ext, size)
		if err != nil {
			return record{}, err
		}
		var hit *record
		for j := range recs {
			if recs[j].name == part {
				hit = &recs[j]
				break
			}
		}
		if hit == nil {
			return record{}, layoutf("%s: %q not found (joliet=%v)", path, part, t.joliet)
		}
		if i == len(parts)-1 {
			return *hit, nil
		}
		if !hit.isDir() {
			return record{}, layoutf("%s: %q is not a directory", path, part)
		}
		ext, size = hit.extent(), hit.size()
	}
	return record{}, layoutf("%s: empty path", path)
}

func both32(v uint32) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:], v)
	binary.BigEndian.PutUint32(b[4:], v)
	return b
}

func alloc(n uint32) uint32 { return (n + sec - 1) / sec * sec }

// replace swaps the content of an existing file within its allocated sectors.
func (im *image) replace(path string, data []byte) error {
	var ext uint32
	var recs []record
	for _, t := range im.trees {
		r, err := im.lookup(t, path)
		if err != nil {
			return err
		}
		if r.isDir() {
			return layoutf("%s is a directory", path)
		}
		if len(recs) > 0 && r.extent() != ext {
			return layoutf("%s: trees disagree on extent", path)
		}
		ext = r.extent()
		recs = append(recs, r)
	}
	// An empty file has no sectors of its own, so it cannot take any data.
	room := alloc(recs[0].size())
	if uint32(len(data)) > room {
		return layoutf("%s: %d bytes do not fit into %d allocated", path, len(data), room)
	}
	buf := make([]byte, room)
	copy(buf, data)
	if err := im.writeAt(int64(ext)*sec, buf); err != nil {
		return err
	}
	for _, r := range recs {
		if err := im.writeAt(r.off+10, both32(uint32(len(data)))); err != nil {
			return err
		}
	}
	return nil
}

type newFile struct {
	name string
	data []byte
	ext  uint32
}

func isoName(name string) string {
	base, ext, _ := strings.Cut(strings.ToUpper(name), ".")
	clean := func(s string, n int) string {
		var b strings.Builder
		for _, c := range s {
			if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
				b.WriteRune(c)
			} else {
				b.WriteRune('_')
			}
		}
		s = b.String()
		if len(s) > n {
			s = s[:n]
		}
		return s
	}
	return clean(base, 8) + "." + clean(ext, 3) + ";1"
}

func buildRecord(tmpl []byte, joliet bool, f newFile) ([]byte, error) {
	var id []byte
	if joliet {
		for _, u := range utf16.Encode([]rune(f.name + ";1")) {
			id = binary.BigEndian.AppendUint16(id, u)
		}
	} else {
		id = []byte(isoName(f.name))
	}
	var su []byte
	if !joliet {
		err := susp(tmpl, func(sig string, e []byte) {
			if sig == "NM" {
				return
			}
			su = append(su, e...)
		})
		if err != nil {
			return nil, err
		}
		if 5+len(f.name) > 255 {
			return nil, layoutf("file name %q is too long for Rock Ridge", f.name)
		}
		su = append(su, 'N', 'M', byte(5+len(f.name)), 1, 0)
		su = append(su, f.name...)
	} else {
		su = append(su, tmpl[suOffset(tmpl):]...)
	}
	r := append([]byte(nil), tmpl[:33]...)
	r[32] = byte(len(id))
	r = append(r, id...)
	if len(id)%2 == 0 {
		r = append(r, 0)
	}
	r = append(r, su...)
	if len(r)%2 == 1 {
		r = append(r, 0)
	}
	if len(r) > 255 {
		return nil, layoutf("directory record for %s is too long", f.name)
	}
	r[0] = byte(len(r))
	copy(r[2:10], both32(f.ext))
	copy(r[10:18], both32(uint32(len(f.data))))
	r[25] = 0 // plain file
	return r, nil
}

// donate removes the root file `donor` and places `files` in its sectors,
// adding a root directory record for each one in every tree.
func (im *image) donate(donor string, files []newFile) error {
	var donorExt, donorRoom uint32
	for i, t := range im.trees {
		r, err := im.lookup(t, donor)
		if err != nil {
			return err
		}
		if r.isDir() {
			return layoutf("donor %s is a directory", donor)
		}
		if i > 0 && r.extent() != donorExt {
			return layoutf("donor %s: trees disagree on extent", donor)
		}
		donorExt, donorRoom = r.extent(), alloc(r.size())
	}
	next := donorExt
	var used uint32
	for i := range files {
		need := alloc(uint32(len(files[i].data)))
		if need == 0 {
			need = sec
		}
		if used+need > donorRoom {
			return layoutf("files need more than the %d bytes of %s", donorRoom, donor)
		}
		files[i].ext = next
		buf := make([]byte, need)
		copy(buf, files[i].data)
		if err := im.writeAt(int64(next)*sec, buf); err != nil {
			return err
		}
		next += need / sec
		used += need
	}
	// wipe the rest of the donor so no stale checksums linger
	if rest := donorRoom - used; rest > 0 {
		if err := im.writeAt(int64(next)*sec, make([]byte, rest)); err != nil {
			return err
		}
	}

	for _, t := range im.trees {
		recs, err := im.readDir(t, t.rootExt, t.rootLen)
		if err != nil {
			return err
		}
		if len(recs) < 2 || recs[0].name != "." || recs[1].name != ".." {
			return layoutf("root directory of joliet=%v has no . and .. entries first", t.joliet)
		}
		var keep [][]byte
		var tmpl []byte
		for _, r := range recs {
			if r.name == donor {
				tmpl = r.raw
				continue
			}
			for _, f := range files {
				if r.name == f.name {
					return layoutf("%s already exists in the root directory", f.name)
				}
			}
			keep = append(keep, r.raw)
		}
		if tmpl == nil {
			return layoutf("donor %s is not in the root directory", donor)
		}
		for _, f := range files {
			nr, err := buildRecord(tmpl, t.joliet, f)
			if err != nil {
				return err
			}
			keep = append(keep, nr)
		}
		// ".", ".." first, then by identifier bytes
		sort.SliceStable(keep[2:], func(a, b int) bool {
			ia := keep[2+a][33 : 33+int(keep[2+a][32])]
			ib := keep[2+b][33 : 33+int(keep[2+b][32])]
			return bytes.Compare(ia, ib) < 0
		})
		out := make([]byte, 0, t.rootLen)
		for _, r := range keep {
			if len(out)%sec+len(r) > sec {
				out = append(out, make([]byte, sec-len(out)%sec)...)
			}
			out = append(out, r...)
		}
		if uint32(len(out)) > t.rootLen {
			return layoutf("root directory overflows (%d > %d, joliet=%v)", len(out), t.rootLen, t.joliet)
		}
		out = append(out, make([]byte, int(t.rootLen)-len(out))...)
		if err := im.writeAt(int64(t.rootExt)*sec, out); err != nil {
			return err
		}
	}
	return nil
}

// setLabel writes the primary label and the Joliet label. An empty label is left unchanged.
func (im *image) setLabel(label, jolietLabel string) error {
	if label == "" && jolietLabel == "" {
		return nil
	}
	for s := int64(16); s < 64; s++ {
		d, err := im.readAt(s*sec, sec)
		if err != nil {
			return err
		}
		switch {
		case d[0] == 255:
			return nil
		case d[0] == 1 && label != "":
			b := []byte(label)
			for len(b) < 32 {
				b = append(b, ' ')
			}
			if err := im.writeAt(s*sec+40, b); err != nil {
				return err
			}
		case d[0] == 2 && jolietLabel != "" && isJoliet(d):
			units := utf16.Encode([]rune(jolietLabel))
			for len(units) < 16 {
				units = append(units, ' ')
			}
			var b []byte
			for _, u := range units {
				b = binary.BigEndian.AppendUint16(b, u)
			}
			if err := im.writeAt(s*sec+40, b); err != nil {
				return err
			}
		}
	}
	return layoutf("no volume descriptor terminator")
}
