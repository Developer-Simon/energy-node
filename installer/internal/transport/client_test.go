package transport

import "testing"

func TestBuildCommandQuotesAndSortsEnv(t *testing.T) {
	got := BuildCommand(map[string]string{
		"EN_BUNDLE_DIR": "/opt/en/bundle",
		"EN_STATE_DIR":  "/var/lib/it's/state",
	}, "bash /opt/en/bundle/bootstrap/10-apt.sh")

	want := `EN_BUNDLE_DIR='/opt/en/bundle' EN_STATE_DIR='/var/lib/it'\''s/state' bash /opt/en/bundle/bootstrap/10-apt.sh`
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestBuildCommandWithoutEnv(t *testing.T) {
	got := BuildCommand(nil, "bash /opt/en/bundle/bootstrap/plan.sh")
	want := "bash /opt/en/bundle/bootstrap/plan.sh"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildCommandIsDeterministic(t *testing.T) {
	env := map[string]string{"B": "2", "A": "1", "C": "3"}
	first := BuildCommand(env, "true")
	for i := 0; i < 5; i++ {
		if got := BuildCommand(env, "true"); got != first {
			t.Fatalf("BuildCommand is not deterministic: %q vs %q", got, first)
		}
	}
}

func TestAuthMethodRequiresPasswordOrKey(t *testing.T) {
	if _, err := authMethod(Config{User: "pi"}); err == nil {
		t.Fatalf("expected an error without a password or a private key")
	}
}

func TestAuthMethodRejectsUnparsablePrivateKey(t *testing.T) {
	_, err := authMethod(Config{User: "pi", PrivateKeyPEM: []byte("not a key")})
	if err == nil {
		t.Fatalf("expected an error for an unparsable private key")
	}
}
