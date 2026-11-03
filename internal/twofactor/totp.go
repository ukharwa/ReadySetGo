package twofactor

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // TOTP (RFC 6238) is defined with SHA-1, and every authenticator app expects it
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"net/url"
	"time"
)

// Codes from authenticator apps: 6 digits, a new one every 30 seconds.
const (
	period = 30
	digits = 6
)

// step returns the 30-second time step t falls in.
func step(t time.Time) int64 { return t.Unix() / period }

// code returns the code for secret in time step n (RFC 6238).
func code(secret []byte, n int64) string {
	mac := hmac.New(sha1.New, secret)
	mac.Write(binary.BigEndian.AppendUint64(nil, uint64(n))) //nolint:gosec // time steps are positive
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[offset:]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1_000_000)
}

// matchStep returns the time step whose code is c, allowing one step either
// side of now for a phone's clock being a little out.
func matchStep(secret []byte, c string, now time.Time) (int64, bool) {
	for n := step(now) - 1; n <= step(now)+1; n++ {
		if subtle.ConstantTimeCompare([]byte(code(secret, n)), []byte(c)) == 1 {
			return n, true
		}
	}
	return 0, false
}

// keyURI is the otpauth:// link an authenticator app reads from the QR code.
func keyURI(issuer, account, secret string) string {
	u := url.URL{
		Scheme:   "otpauth",
		Host:     "totp",
		Path:     "/" + issuer + ":" + account,
		RawQuery: url.Values{"secret": {secret}, "issuer": {issuer}}.Encode(),
	}
	return u.String()
}
