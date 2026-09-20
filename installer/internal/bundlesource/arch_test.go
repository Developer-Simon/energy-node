package bundlesource

import "testing"

func TestArchForMachine(t *testing.T) {
	cases := []struct {
		machine string
		arch    string
		ok      bool
	}{
		{"armv6l", "armv6", true},
		{"armv7l\n", "armv6", true},
		{"aarch64", "arm64", true},
		{"arm64", "arm64", true},
		{"x86_64", "amd64", true},
		{"riscv64", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		arch, ok := ArchForMachine(c.machine)
		if arch != c.arch || ok != c.ok {
			t.Errorf("ArchForMachine(%q) = %q, %v; want %q, %v", c.machine, arch, ok, c.arch, c.ok)
		}
	}
}
