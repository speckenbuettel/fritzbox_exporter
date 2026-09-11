package fritzbox_upnp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSOAPRenewsDigestAfterRouterRestart(t *testing.T) {
	authHeaderMu.Lock()
	saved := authHeader
	authHeader = `Digest nonce="before-reboot"`
	authHeaderMu.Unlock()
	defer func() { authHeaderMu.Lock(); authHeader = saved; authHeaderMu.Unlock() }()
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.Contains(r.Header.Get("Authorization"), `nonce="after-reboot"`) {
			w.Header().Set("WWW-Authenticate", `Digest realm="fritz", nonce="after-reboot", qop="auth", algorithm=MD5`)
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetInfoResponse xmlns:u="urn:test"><NewValue>1</NewValue></u:GetInfoResponse></s:Body></s:Envelope>`)
	}))
	defer s.Close()
	root := &Root{BaseURL: s.URL, Username: "exporter", Password: "test"}
	service := &Service{Device: &Device{root: root}, ControlURL: "/control", ServiceType: "urn:test"}
	action := &Action{service: service, Name: "GetInfo"}
	if _, err := action.CallWithClient(nil, s.Client()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}
