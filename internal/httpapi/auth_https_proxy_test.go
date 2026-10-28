package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
)

// Real TLS termination and a separate private HTTP hop. No SQL, provider,
// real-domain credentials, persisted certificates or relaxed TLS verification.
func TestAuthHTTPSProxySessionFlow(t *testing.T) {
	manager, e := auth.NewManager("CI_https_proxy_synthetic_session_secret_320", map[string]string{"eugene": "CI_pw", "nastya": "CI_pw", "demo": "CI_pw"})
	if e != nil {
		t.Fatal(e)
	}
	backend := httptest.NewServer(NewHandlerWithOptions(nil, Options{Auth: manager}))
	defer backend.Close()
	target, e := url.Parse(backend.URL)
	if e != nil {
		t.Fatal(e)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		host := r.Host
		director(r)
		r.Host = host
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Del("X-Forwarded-Host")
	}
	front := httptest.NewTLSServer(proxy)
	defer front.Close()
	frontURL, e := url.Parse(front.URL)
	if e != nil {
		t.Fatal(e)
	}
	frontPort := frontURL.Port()
	backPort := target.Port()
	frontURL.Host = net.JoinHostPort("example.com", frontPort)
	frontAddress := frontURL.String()
	backAddress := "http://" + net.JoinHostPort("example.com", backPort)
	if e := front.Certificate().VerifyHostname("example.com"); e != nil {
		t.Fatal("TLS fixture lacks expected test hostname")
	}
	// A public test hostname avoids the loopback Secure-cookie exception used by
	// browsers/newer Go jars. Dial only these two loopback ports; no external DNS/network.
	transport := front.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil || host != "example.com" || port != frontPort && port != backPort {
			return nil, errors.New("unexpected auth test target")
		}
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
	}
	defer transport.CloseIdleConnections()

	for _, login := range []string{"eugene", "nastya", "demo"} {
		t.Run(login, func(t *testing.T) {
			client := &http.Client{Transport: transport} // Trust the fixture certificate; verification stays enabled.
			client.Timeout = 3 * time.Second
			client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
			client.Jar, e = cookiejar.New(nil)
			if e != nil {
				t.Fatal(e)
			}
			send := func(method, address, origin string, form url.Values) *http.Response {
				t.Helper()
				var body io.Reader
				if form != nil {
					body = strings.NewReader(form.Encode())
				}
				r, e := http.NewRequest(method, address, body)
				if e != nil {
					t.Fatal(e)
				}
				if form != nil {
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
				if origin != "" {
					r.Header.Set("Origin", origin)
				}
				response, e := client.Do(r)
				if e != nil {
					t.Fatal("synthetic HTTPS/proxy request failed")
				}
				t.Cleanup(func() { response.Body.Close() })
				return response
			}
			expectStatus := func(response *http.Response, want int) {
				t.Helper()
				if response.StatusCode != want {
					t.Fatalf("HTTPS/proxy status %d expected %d", response.StatusCode, want)
				}
			}
			expectStatus(send("GET", frontAddress+"/api/session", "", nil), 401)
			bad := send("POST", frontAddress+"/auth", frontAddress, url.Values{"login": {login}, "password": {"CI_invalid"}})
			expectStatus(bad, 401)
			if bad.Header.Get("X-App-Login-Error") != "1" {
				t.Fatal("proxy lost login failure header")
			}
			authenticate := func() {
				t.Helper()
				response := send("POST", frontAddress+"/auth", frontAddress, url.Values{"login": {login}, "password": {"CI_pw"}})
				expectStatus(response, 303)
				if response.Header.Get("Location") != "/" {
					t.Fatal("proxy changed login redirect")
				}
				var cookie *http.Cookie
				for _, c := range response.Cookies() {
					if c.Name == auth.CookieName {
						cookie = c
					}
				}
				if cookie == nil || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.MaxAge != int(auth.SessionTTL.Seconds()) {
					t.Fatal("proxy lost auth cookie attributes")
				}
			}
			authenticate()
			response := send("GET", frontAddress+"/api/session", "", nil)
			expectStatus(response, 200)
			var user auth.User
			if e = json.NewDecoder(response.Body).Decode(&user); e != nil {
				t.Fatal("invalid synthetic session response")
			}
			expected, ok := manager.Authenticate(login, "CI_pw")
			if !ok || user != expected {
				t.Fatal("proxy changed authenticated role")
			}
			// The jar must not send this Secure cookie over the private cleartext origin.
			expectStatus(send("GET", backAddress+"/api/session", "", nil), 401)
			expectStatus(send("POST", frontAddress+"/auth", "https://other.example.invalid", url.Values{"login": {login}, "password": {"CI_pw"}}), 403)
			issued, e := manager.Issue(expected, time.Now())
			if e != nil {
				t.Fatal(e)
			}
			client.Jar.SetCookies(frontURL, []*http.Cookie{{Name: auth.CookieName, Value: issued + "invalid", Path: "/", Secure: true}})
			expectStatus(send("GET", frontAddress+"/api/session", "", nil), 401)
			expired, e := manager.Issue(expected, time.Now().Add(-auth.SessionTTL-time.Hour))
			if e != nil {
				t.Fatal(e)
			}
			client.Jar.SetCookies(frontURL, []*http.Cookie{{Name: auth.CookieName, Value: expired, Path: "/", Secure: true}})
			expectStatus(send("GET", frontAddress+"/api/session", "", nil), 401)
			authenticate()
			expectStatus(send("GET", frontAddress+"/api/session", "", nil), 200)
			logout := send("GET", frontAddress+"/logout", "", nil)
			expectStatus(logout, 303)
			cleared := false
			for _, c := range logout.Cookies() {
				if c.Name == auth.CookieName && c.MaxAge < 0 {
					cleared = true
				}
			}
			if !cleared {
				t.Fatal("proxy lost logout cookie deletion")
			}
			expectStatus(send("GET", frontAddress+"/api/session", "", nil), 401)
		})
	}
}
