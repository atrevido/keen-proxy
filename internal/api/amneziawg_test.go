package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAmneziaWGPreviewStripsSubscription(t *testing.T) {
	server, _ := newServer()
	profile := `# vpn://secret-test-subscription
[Interface]
PrivateKey = IBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBA=
Address = 198.18.0.3/32
[Peer]
PublicKey = EBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBA=
AllowedIPs = 0.0.0.0/0
Endpoint = 192.0.2.10:51820
`
	for _, tc := range []struct {
		name string
		raw  string
		code int
	}{
		{"valid", profile, http.StatusOK},
		{"hook", strings.Replace(profile, "Address =", "PostUp = secret-test-subscription\nAddress =", 1), http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]string{"profile": tc.raw})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/connections/amneziawg/preview", bytes.NewReader(raw))
			req.RemoteAddr = "127.0.0.1:1234"
			req.Header.Set("Authorization", "Bearer "+server.Token)
			res := httptest.NewRecorder()
			server.Handler().ServeHTTP(res, req)
			if res.Code != tc.code || strings.Contains(res.Body.String(), "secret-test-subscription") {
				t.Fatalf("unexpected response: %d", res.Code)
			}
			if tc.code == http.StatusOK && !strings.Contains(res.Body.String(), "192.0.2.10:51820") {
				t.Fatal("preview is missing endpoint")
			}
		})
	}
}
