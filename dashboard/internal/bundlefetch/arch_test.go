package bundlefetch

import "testing"

func TestArchForGo(t *testing.T) {
	cases := []struct {
		goarch string
		arch   string
		ok     bool
	}{
		{"arm", "armv6", true},
		{"arm64", "arm64", true},
		{"amd64", "amd64", true},
		{"riscv64", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		arch, ok := ArchForGo(c.goarch)
		if arch != c.arch || ok != c.ok {
			t.Errorf("ArchForGo(%q) = %q, %v; want %q, %v", c.goarch, arch, ok, c.arch, c.ok)
		}
	}
}
