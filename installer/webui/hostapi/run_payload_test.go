package hostapi

import (
	"errors"
	"testing"
)

func TestRunFinishPayloadCarriesTheDetailOfEveryFailure(t *testing.T) {
	redactor := NewRedactor("hunter2-secret")
	cases := map[string]struct {
		err        error
		wantCode   string
		wantDetail string
	}{
		"untyped": {errors.New("no bootstrap script found for step 10"), "BACKEND_ERROR", "no bootstrap script found for step 10"},
		"typed":   {&Error{Code: "PACKAGE_STAGE_FAILED", Detail: "scp: broken pipe"}, "PACKAGE_STAGE_FAILED", "scp: broken pipe"},
		"secret":  {errors.New("login with hunter2-secret failed"), "BACKEND_ERROR", "login with " + redactor.Line("hunter2-secret") + " failed"},
	}
	for name, tc := range cases {
		payload := runFinishPayload(NewBus(10), "run-1", tc.err, redactor)
		if payload["code"] != tc.wantCode || payload["detail"] != tc.wantDetail {
			t.Errorf("%s: payload = %v, want code %s detail %q", name, payload, tc.wantCode, tc.wantDetail)
		}
	}
}
