package twofactor

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/auth"
)

func newAPIApp(t *testing.T) (*apptest.App, *Service) {
	t.Helper()
	a, svc := newApp(t)
	auth.NewAPI(a.Accounts, a.Limiter).Routes(a.Mux)
	NewAPI(svc, a.Limiter).Routes(a.Mux)
	return a, svc
}

type statusJSON struct {
	Enabled           bool `json:"enabled"`
	RecoveryCodesLeft int  `json:"recovery_codes_left"`
}

type codesJSON struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

func TestAPITwoFactor(t *testing.T) {
	t.Parallel()
	a, _ := newAPIApp(t)
	if r := a.JSON(a.Client(), "GET", "/api/auth/two-factor", "", nil); r.Status != http.StatusUnauthorized {
		t.Errorf("status logged out: %d", r.Status)
	}
	c := a.Client()
	u := a.SignIn(c, "ada@example.com")

	var st statusJSON
	a.Decode(a.JSON(c, "GET", "/api/auth/two-factor", "", nil), &st)
	if st.Enabled {
		t.Errorf("status before turning on: %+v", st)
	}
	var setup struct{ Secret, URI string }
	a.Decode(a.JSON(c, "GET", "/api/auth/two-factor/setup", "", nil), &setup)
	if !strings.HasPrefix(setup.URI, "otpauth://totp/Test:ada@example.com?") || !strings.Contains(setup.URI, setup.Secret) {
		t.Fatalf("setup = %+v", setup)
	}
	r := a.JSON(c, "POST", "/api/auth/two-factor/enable", "", map[string]string{"secret": setup.Secret, "code": "000000", "password": apptest.Password})
	if r.Status != http.StatusUnprocessableEntity || !strings.Contains(string(r.Body), `"code"`) {
		t.Errorf("wrong code: %d %s", r.Status, r.Body)
	}
	var codes codesJSON
	a.Decode(a.JSON(c, "POST", "/api/auth/two-factor/enable", "", map[string]string{
		"secret": setup.Secret, "code": codeFor(t, setup.Secret, 0), "password": apptest.Password,
	}), &codes)
	if len(codes.RecoveryCodes) != 10 {
		t.Fatalf("enable: %+v", codes)
	}
	if r := a.JSON(c, "POST", "/api/auth/two-factor/enable", "", map[string]string{
		"secret": setup.Secret, "code": codeFor(t, setup.Secret, 1), "password": apptest.Password,
	}); r.Status != http.StatusConflict {
		t.Errorf("enable twice: %d", r.Status)
	}

	// A token client gets a challenge for the password, then a token for
	// the code.
	login := map[string]string{"email": u.Email, "password": apptest.Password}
	var step struct{ Challenge string }
	r = a.JSON(a.Client(), "POST", "/api/auth/token", "", login)
	if a.Decode(r, &step); r.Status != http.StatusAccepted || step.Challenge == "" {
		t.Fatalf("token with password: %d %s", r.Status, r.Body)
	}
	r = a.JSON(a.Client(), "POST", "/api/auth/token", "", map[string]string{"challenge": step.Challenge, "code": "000000"})
	if r.Status != http.StatusUnauthorized {
		t.Errorf("wrong code: %d %s", r.Status, r.Body)
	}
	var issued struct{ Token string }
	a.Decode(a.JSON(a.Client(), "POST", "/api/auth/token", "", map[string]string{"challenge": step.Challenge, "code": codeFor(t, setup.Secret, 1)}), &issued)
	if r := a.JSON(a.Client(), "GET", "/api/auth/me", issued.Token, nil); r.Status != http.StatusOK {
		t.Errorf("me with the token: %d", r.Status)
	}

	// A browser frontend does the same for its cookie, here with a
	// recovery code.
	browser := a.Client()
	a.Decode(a.JSON(browser, "POST", "/api/auth/login", "", login), &step)
	r = a.JSON(browser, "POST", "/api/auth/login", "", map[string]any{"challenge": step.Challenge, "code": codes.RecoveryCodes[0], "remember": true})
	if r.Status != http.StatusNoContent || !strings.Contains(r.Header.Get("Set-Cookie"), "__Host-session=") {
		t.Fatalf("login with a recovery code: %d %s", r.Status, r.Body)
	}
	if r := a.JSON(browser, "POST", "/api/auth/login", "", map[string]string{"challenge": "forged", "code": "000000"}); r.Status != http.StatusUnauthorized {
		t.Errorf("forged challenge: %d", r.Status)
	}

	a.Decode(a.JSON(c, "GET", "/api/auth/two-factor", "", nil), &st)
	if !st.Enabled || st.RecoveryCodesLeft != 9 {
		t.Errorf("status = %+v", st)
	}
	if r := a.JSON(c, "POST", "/api/auth/two-factor/recovery-codes", "", map[string]string{"password": "not the password"}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("new codes, wrong password: %d", r.Status)
	}
	a.Decode(a.JSON(c, "POST", "/api/auth/two-factor/recovery-codes", "", map[string]string{"password": apptest.Password}), &codes)
	if len(codes.RecoveryCodes) != 10 {
		t.Errorf("new codes: %+v", codes)
	}
	if r := a.JSON(c, "POST", "/api/auth/two-factor/disable", "", map[string]string{"password": apptest.Password}); r.Status != http.StatusNoContent {
		t.Errorf("turn off: %d %s", r.Status, r.Body)
	}
	if r := a.JSON(a.Client(), "POST", "/api/auth/token", "", login); r.Status != http.StatusOK {
		t.Errorf("token after turning off: %d, want a token straight away", r.Status)
	}
}
