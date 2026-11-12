package media

import (
	"strings"
	"testing"
)

func testSettings() Settings {
	return Settings{
		Hostname:      "energy-node",
		User:          "simon",
		AuthorizedKey: "ssh-ed25519 AAAAC3Nza installer",
		Timezone:      "Europe/Berlin",
		Keyboard:      "de",
	}
}

func testHostKey(t *testing.T) HostKey {
	t.Helper()
	hk, err := NewHostKey()
	if err != nil {
		t.Fatalf("NewHostKey: %v", err)
	}
	return hk
}

func TestUserDataLocksThePasswordByDefault(t *testing.T) {
	out := string(UserData(testSettings(), testHostKey(t)))
	for _, want := range []string{
		"#cloud-config\n",
		"lock_passwd: true",
		"ssh_pwauth: false",
		"sudo: ALL=(ALL) NOPASSWD:ALL",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("UserData missing %q", want)
		}
	}
	if strings.Contains(out, "\n  passwd:") {
		t.Error("UserData contains passwd: although no hash is set")
	}
	if !strings.HasPrefix(out, "#cloud-config\n") {
		t.Errorf("UserData does not start with #cloud-config")
	}
}

func TestUserDataCarriesTheEmergencyPassword(t *testing.T) {
	s := testSettings()
	s.PasswordHash = "$6$abc$def"
	out := string(UserData(s, testHostKey(t)))
	if !strings.Contains(out, "lock_passwd: false") {
		t.Error("lock_passwd is not false")
	}
	if !strings.Contains(out, `passwd: "$6$abc$def"`) {
		t.Error("passwd line with hash missing")
	}
}

func TestUserDataEmbedsTheHostKey(t *testing.T) {
	hk := testHostKey(t)
	out := string(UserData(testSettings(), hk))
	if !strings.Contains(out, "  ed25519_private: |\n") {
		t.Fatal("ed25519_private block missing")
	}
	for _, line := range strings.Split(strings.TrimRight(string(hk.PrivatePEM), "\n"), "\n") {
		if !strings.Contains(out, "\n    "+line+"\n") {
			t.Errorf("PEM line %q not indented by exactly 4 spaces", line)
		}
	}
	if !strings.Contains(out, `ed25519_public: "ssh-ed25519 `) {
		t.Error("ed25519_public line missing")
	}
	if !strings.Contains(out, "ssh_genkeytypes: []") {
		t.Error("ssh_genkeytypes: [] missing")
	}
}

func TestUserDataQuotesHostileValues(t *testing.T) {
	s := testSettings()
	s.Hostname = `a"b\c`
	out := string(UserData(s, testHostKey(t)))
	if !strings.Contains(out, `hostname: "a\"b\\c"`) {
		t.Errorf("hostname not escaped, got:\n%s", out)
	}
}

func TestNetworkConfigOnlyWithWifi(t *testing.T) {
	s := testSettings()
	if NetworkConfig(s) != nil {
		t.Fatal("NetworkConfig without Wifi is not nil")
	}
	s.Wifi = &Wifi{SSID: "Werkstatt", Password: "pw", Country: "DE"}
	out := string(NetworkConfig(s))
	if !strings.Contains(out, `regulatory-domain: "DE"`) {
		t.Error("regulatory-domain missing")
	}
	if !strings.Contains(out, `"Werkstatt":`) {
		t.Error("SSID key missing")
	}
}

func TestFirstRunScriptQuotesForTheShell(t *testing.T) {
	s := testSettings()
	s.User = "it's"
	out := string(FirstRunScript(s, testHostKey(t)))
	if !strings.Contains(out, `'it'\''s'`) {
		t.Error("user not shell-quoted with '\\'' escape")
	}
	if !strings.HasSuffix(out, "exit 0\n") {
		t.Error("script does not end with exit 0\\n")
	}
	iRm := strings.Index(out, "rm -f /etc/ssh/ssh_host_*")
	iCat := strings.Index(out, "cat >/etc/ssh/ssh_host_ed25519_key")
	if iRm < 0 || iCat < 0 || iRm > iCat {
		t.Errorf("rm -f must precede cat >/etc/ssh/ssh_host_ed25519_key (rm=%d cat=%d)", iRm, iCat)
	}
}

func TestFirstRunScriptSkipsWifiWithoutIt(t *testing.T) {
	out := string(FirstRunScript(testSettings(), testHostKey(t)))
	if strings.Contains(out, "set_wlan") {
		t.Error("set_wlan present without Wifi")
	}
}

func TestFirstRunScriptIncludesWifiWhenSet(t *testing.T) {
	s := testSettings()
	s.Wifi = &Wifi{SSID: "Werkstatt", Password: "pw", Country: "DE"}
	out := string(FirstRunScript(s, testHostKey(t)))
	if !strings.Contains(out, `set_wlan -p 'Werkstatt' 'pw' 'DE'`) {
		t.Error("set_wlan line with quoted values missing")
	}
}

func TestUserDataSetsTheWifiCountry(t *testing.T) {
	s := testSettings()
	if strings.Contains(string(UserData(s, testHostKey(t))), "bootcmd:") {
		t.Error("bootcmd present without Wifi")
	}
	s.Wifi = &Wifi{SSID: "Werkstatt", Password: "pw", Country: "DE"}
	out := string(UserData(s, testHostKey(t)))
	want := "bootcmd:\n  - [ cloud-init-per, once, en-wifi-country, sh, -c, \"raspi-config nonint do_wifi_country DE; rfkill unblock wifi\" ]\n"
	if !strings.Contains(out, want) {
		t.Errorf("user-data lacks the country bootcmd:\n%s", out)
	}
}
