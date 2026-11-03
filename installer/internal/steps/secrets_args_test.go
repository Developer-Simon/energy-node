package steps

import "testing"

func TestStep60UserArgs(t *testing.T) {
	full := &Secrets{MQTTPassword: "m", AdminPassword: "a", AdminUser: "chef"}
	if got := step60UserArgs("broker", full); got != " --mqtt-user 'broker' --admin-user 'chef'" {
		t.Errorf("full = %q", got)
	}
	if got := step60UserArgs("", &Secrets{AdminUser: "chef"}); got != "" {
		t.Errorf("without admin password = %q, want empty", got)
	}
	if got := step60UserArgs("", &Secrets{AdminPassword: "a"}); got != "" {
		t.Errorf("without admin user = %q, want empty", got)
	}
}
