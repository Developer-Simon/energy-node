package isopatch

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsoName(t *testing.T) {
	cases := map[string]string{
		"preseed.cfg":    "PRESEED.CFG;1",
		"energy-node.sh": "ENERGY_N.SH;1",
		"longfile.html":  "LONGFILE.HTM;1",
	}
	for in, want := range cases {
		if got := isoName(in); got != want {
			t.Errorf("isoName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAlloc(t *testing.T) {
	cases := map[uint32]uint32{0: 0, 1: 2048, 2047: 2048, 2048: 2048, 2049: 4096}
	for in, want := range cases {
		if got := alloc(in); got != want {
			t.Errorf("alloc(%d) = %d, want %d", in, got, want)
		}
	}
}

// testTemplate is a root record with a PX and an NM entry, like a Debian netinst record.
func testTemplate() []byte {
	id := []byte("MD5SUM.TXT;1")
	r := make([]byte, 33)
	r[32] = byte(len(id))
	r = append(r, id...)
	r = append(r, 0) // pad, identifier length is even
	r = append(r, 'P', 'X', 4, 1)
	r = append(r, 'N', 'M', 5+10, 1, 0)
	r = append(r, "md5sum.txt"...)
	r[0] = byte(len(r))
	return r
}

func TestBuildRecord(t *testing.T) {
	tmpl := testTemplate()
	for _, joliet := range []bool{false, true} {
		rec, err := buildRecord(tmpl, joliet, newFile{name: "preseed.cfg", data: []byte("hello"), ext: 4242})
		if err != nil {
			t.Fatalf("joliet=%v: buildRecord: %v", joliet, err)
		}
		if int(rec[0]) != len(rec) || len(rec)%2 != 0 {
			t.Errorf("joliet=%v: length byte %d for %d bytes, want the same and even", joliet, rec[0], len(rec))
		}
		if got := binary.LittleEndian.Uint32(rec[2:6]); got != 4242 {
			t.Errorf("joliet=%v: extent = %d, want 4242", joliet, got)
		}
		if got := binary.BigEndian.Uint32(rec[6:10]); got != 4242 {
			t.Errorf("joliet=%v: big-endian extent = %d, want 4242", joliet, got)
		}
		if got := binary.LittleEndian.Uint32(rec[10:14]); got != 5 {
			t.Errorf("joliet=%v: size = %d, want 5", joliet, got)
		}
		if rec[25]&2 != 0 {
			t.Errorf("joliet=%v: record is marked as directory", joliet)
		}
		name, err := recName(rec, joliet)
		if err != nil {
			t.Fatalf("joliet=%v: recName: %v", joliet, err)
		}
		if name != "preseed.cfg" {
			t.Errorf("joliet=%v: name = %q, want preseed.cfg", joliet, name)
		}
	}
}

func TestBuildRecordKeepsNoOldNameEntry(t *testing.T) {
	rec, err := buildRecord(testTemplate(), false, newFile{name: "a.txt", data: nil, ext: 1})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rec, []byte("md5sum.txt")) {
		t.Errorf("record still carries the donor name: %q", rec)
	}
}

func TestValidateLabels(t *testing.T) {
	ok := []Options{
		{},
		{Label: strings.Repeat("A", 32)},
		{JolietLabel: strings.Repeat("B", 16)},
	}
	for _, o := range ok {
		if err := validate(o); err != nil {
			t.Errorf("validate(%+v) = %v, want nil", o, err)
		}
	}
	bad := []Options{
		{Label: strings.Repeat("A", 33)},
		{JolietLabel: strings.Repeat("B", 17)},
	}
	for _, o := range bad {
		err := validate(o)
		if !errors.Is(err, ErrLayout) {
			t.Errorf("validate(%+v) = %v, want ErrLayout", o, err)
		}
	}
}

func TestPatchRefusesBogusInputAndRemovesOutput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "bogus.iso")
	out := filepath.Join(dir, "out.iso")
	if err := os.WriteFile(in, make([]byte, 100000), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Patch(in, out, Options{Add: []File{{Name: "preseed.cfg", Data: []byte("x")}}})
	if !errors.Is(err, ErrLayout) {
		t.Fatalf("Patch(bogus) = %v, want ErrLayout", err)
	}
	if _, statErr := os.Stat(out); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("output still exists after error: %v", statErr)
	}
}

func TestPatchRemovesOutputOnLabelError(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "bogus.iso")
	out := filepath.Join(dir, "out.iso")
	if err := os.WriteFile(in, make([]byte, 100000), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Patch(in, out, Options{Label: strings.Repeat("A", 33)})
	if !errors.Is(err, ErrLayout) {
		t.Fatalf("Patch(long label) = %v, want ErrLayout", err)
	}
	if _, statErr := os.Stat(out); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("output still exists after error: %v", statErr)
	}
}

func TestPatchMissingInputRemovesOutput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.iso")
	if err := os.WriteFile(out, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Patch(filepath.Join(dir, "missing.iso"), out, Options{})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Patch(missing) = %v, want ErrNotExist", err)
	}
	if _, statErr := os.Stat(out); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("output still exists after error: %v", statErr)
	}
}

func TestPatchNeverOverwritesItsInput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "bogus.iso")
	content := make([]byte, 100000)
	content[5] = 7
	if err := os.WriteFile(in, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Patch(in, in, Options{Add: []File{{Name: "a.txt", Data: []byte("x")}}}); err == nil {
		t.Fatal("Patch(in, in) succeeded")
	}
	got, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Error("input was changed by a refused Patch(in, in)")
	}
}

// isoReader reads ISO 9660 with Rock Ridge names. It is independent of the code under test.
type isoReader struct{ f *os.File }

type entry struct {
	name string
	ext  uint32
	size uint32
	dir  bool
}

func (r isoReader) bytesAt(t *testing.T, off, n int64) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := r.f.ReadAt(b, off); err != nil {
		t.Fatalf("read %d bytes at %d: %v", n, off, err)
	}
	return b
}

func (r isoReader) root(t *testing.T) entry {
	t.Helper()
	pvd := r.bytesAt(t, 16*sec, sec)
	if string(pvd[1:6]) != "CD001" || pvd[0] != 1 {
		t.Fatal("sector 16 is not a primary volume descriptor")
	}
	root := pvd[156:190]
	return entry{ext: binary.LittleEndian.Uint32(root[2:6]), size: binary.LittleEndian.Uint32(root[10:14]), dir: true}
}

func (r isoReader) dir(t *testing.T, e entry) []entry {
	t.Helper()
	data := r.bytesAt(t, int64(e.ext)*sec, int64(e.size))
	var out []entry
	for p := 0; p < len(data); {
		l := int(data[p])
		if l == 0 {
			p = (p/sec + 1) * sec
			continue
		}
		rec := data[p : p+l]
		p += l
		nl := int(rec[32])
		id := rec[33 : 33+nl]
		if nl == 1 && id[0] <= 1 {
			continue // "." and ".."
		}
		su := rec[33+nl:]
		if nl%2 == 0 {
			su = su[1:]
		}
		name := ""
		for len(su) >= 4 && int(su[2]) >= 4 && int(su[2]) <= len(su) {
			if string(su[:2]) == "NM" {
				name += string(su[5:su[2]])
			}
			su = su[su[2]:]
		}
		if name == "" {
			name = strings.TrimSuffix(string(id), ";1")
		}
		out = append(out, entry{
			name: name,
			ext:  binary.LittleEndian.Uint32(rec[2:6]),
			size: binary.LittleEndian.Uint32(rec[10:14]),
			dir:  rec[25]&2 != 0,
		})
	}
	return out
}

// find returns the entry at path, or false if it does not exist.
func (r isoReader) find(t *testing.T, path string) (entry, bool) {
	t.Helper()
	e := r.root(t)
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		var hit *entry
		for _, c := range r.dir(t, e) {
			if c.name == part {
				c := c
				hit = &c
				break
			}
		}
		if hit == nil {
			return entry{}, false
		}
		e = *hit
	}
	return e, true
}

func (r isoReader) lookup(t *testing.T, path string) entry {
	t.Helper()
	e, ok := r.find(t, path)
	if !ok {
		t.Fatalf("%s not found in image", path)
	}
	return e
}

// files returns every plain file below dir, keyed by its path.
func (r isoReader) files(t *testing.T, dir entry, prefix string) map[string]entry {
	t.Helper()
	out := map[string]entry{}
	for _, c := range r.dir(t, dir) {
		p := prefix + "/" + c.name
		if c.dir {
			for k, v := range r.files(t, c, p) {
				out[k] = v
			}
		} else {
			out[p] = c
		}
	}
	return out
}

func (r isoReader) content(t *testing.T, e entry) []byte {
	t.Helper()
	return r.bytesAt(t, int64(e.ext)*sec, int64(e.size))
}

func TestPatchRealDebianISO(t *testing.T) {
	cases := []struct{ arch, env string }{
		{"amd64", "EN_DEBIAN_ISO"},
		{"arm64", "EN_DEBIAN_ISO_ARM64"},
	}
	ran := false
	for _, c := range cases {
		src := os.Getenv(c.env)
		if src == "" {
			continue
		}
		ran = true
		t.Run(c.arch, func(t *testing.T) { checkRealISO(t, src) })
	}
	if !ran {
		t.Skip("set EN_DEBIAN_ISO and/or EN_DEBIAN_ISO_ARM64 to run against the real netinst images")
	}
}

func checkRealISO(t *testing.T, src string) {
	out := filepath.Join(t.TempDir(), "patched.iso")
	preseed := []byte("# energy-node test preseed\nd-i netcfg/get_hostname string energy-node\n")
	grub := []byte("# energy-node test grub\nset timeout=1\n")

	err := Patch(src, out, Options{
		Label:       "ENERGY-NODE-TEST",
		JolietLabel: "EN-TEST",
		Add:         []File{{Name: "preseed.cfg", Data: preseed}},
		Replace:     map[string][]byte{"/boot/grub/grub.cfg": grub},
	})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}

	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	patched, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer patched.Close()
	si, _ := in.Stat()
	so, _ := patched.Stat()
	if si.Size() != so.Size() {
		t.Errorf("image size changed: %d -> %d", si.Size(), so.Size())
	}

	orig, got := isoReader{in}, isoReader{patched}

	pe := got.lookup(t, "preseed.cfg")
	if !bytes.Equal(got.content(t, pe), preseed) {
		t.Errorf("preseed.cfg in root has wrong content")
	}
	if _, ok := got.find(t, "md5sum.txt"); ok {
		t.Errorf("donor md5sum.txt is still in the root directory")
	}

	ge := got.lookup(t, "/boot/grub/grub.cfg")
	if !bytes.Equal(got.content(t, ge), grub) {
		t.Errorf("/boot/grub/grub.cfg has wrong content: %q", got.content(t, ge))
	}

	head := int64(32 * 1024)
	a := orig.bytesAt(t, 0, head)
	b := got.bytesAt(t, 0, head)
	if !bytes.Equal(a, b) {
		t.Errorf("first 32 KiB differ")
	}

	efi := orig.files(t, orig.lookup(t, "/EFI"), "/EFI")
	if len(efi) == 0 {
		t.Fatal("no files found under /EFI in the source image")
	}
	for p, oe := range efi {
		ne, ok := got.find(t, p)
		if !ok {
			t.Errorf("%s missing in patched image", p)
			continue
		}
		if !bytes.Equal(orig.content(t, oe), got.content(t, ne)) {
			t.Errorf("%s differs from the source image", p)
		}
	}
}

// layoutISO returns the amd64 netinst from EN_DEBIAN_ISO or skips the test.
func layoutISO(t *testing.T) string {
	t.Helper()
	src := os.Getenv("EN_DEBIAN_ISO")
	if src == "" {
		t.Skip("set EN_DEBIAN_ISO to the Debian netinst image to run the layout failure tests")
	}
	return src
}

// expectLayoutFailure runs Patch and checks for an ErrLayout error that mentions want, and that out is removed.
func expectLayoutFailure(t *testing.T, src string, o Options, want string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "failed.iso")
	err := Patch(src, out, o)
	if !errors.Is(err, ErrLayout) {
		t.Fatalf("Patch = %v, want ErrLayout", err)
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}
	if _, statErr := os.Stat(out); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("output still exists after error: %v", statErr)
	}
}

// openISOReader opens the image read-only and returns a reader for it.
func openISOReader(t *testing.T, src string) isoReader {
	t.Helper()
	f, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return isoReader{f}
}

func TestPatchRealISODonorTooSmall(t *testing.T) {
	src := layoutISO(t)
	r := openISOReader(t, src)
	var donor entry
	for _, c := range r.dir(t, r.root(t)) {
		if c.dir || c.size == 0 {
			continue
		}
		if donor.name == "" || c.size < donor.size {
			donor = c
		}
	}
	if donor.name == "" {
		t.Fatal("no non-empty file in the root directory")
	}
	t.Logf("donor %s is %d bytes", donor.name, donor.size)
	// one byte more than the donor's allocated sectors
	big := make([]byte, alloc(donor.size)+1)
	expectLayoutFailure(t, src, Options{Donor: donor.name, Add: []File{{Name: "preseed.cfg", Data: big}}}, "files need more than")
}

func TestPatchRealISODonorMissing(t *testing.T) {
	src := layoutISO(t)
	expectLayoutFailure(t, src, Options{Donor: "no-such-donor.txt", Add: []File{{Name: "preseed.cfg", Data: []byte("x")}}}, "not found")
}

func TestPatchRealISOReplaceTooLarge(t *testing.T) {
	src := layoutISO(t)
	r := openISOReader(t, src)
	grub := r.lookup(t, "/boot/grub/grub.cfg")
	big := make([]byte, alloc(grub.size)+1)
	expectLayoutFailure(t, src, Options{Replace: map[string][]byte{"/boot/grub/grub.cfg": big}}, "do not fit")
}

func TestPatchRealISOReplaceMissingPath(t *testing.T) {
	src := layoutISO(t)
	expectLayoutFailure(t, src, Options{Replace: map[string][]byte{"/no/such/file.cfg": []byte("x")}}, "not found")
}

func TestPatchRealISOPrimaryLabel(t *testing.T) {
	src := layoutISO(t)
	cases := []struct {
		label string
		want  string // 32 bytes in the primary volume descriptor
	}{
		{"ENERGY-NODE", "ENERGY-NODE" + strings.Repeat(" ", 32-len("ENERGY-NODE"))},
		{strings.Repeat("L", 32), strings.Repeat("L", 32)},
	}
	for _, c := range cases {
		out := filepath.Join(t.TempDir(), "label.iso")
		if err := Patch(src, out, Options{Label: c.label}); err != nil {
			t.Fatalf("Patch(label %q): %v", c.label, err)
		}
		f, err := os.Open(out)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]byte, 32)
		_, err = f.ReadAt(got, 16*sec+40)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != c.want {
			t.Errorf("primary label = %q, want %q", got, c.want)
		}
	}
	expectLayoutFailure(t, src, Options{Label: strings.Repeat("L", 33)}, "longer than 32")
}
