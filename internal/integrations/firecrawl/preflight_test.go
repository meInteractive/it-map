package firecrawl

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"testing"
)

func TestPreflightRedirectUsesValidatedAddresses(t *testing.T) {
	addresses := map[string]string{"jobs.company.com": "8.8.8.8", "careers.company.com": "1.1.1.1"}
	lookup := func(_ context.Context, network, host string) ([]netip.Addr, error) {
		if network != "ip" || addresses[host] == "" {
			t.Fatalf("unexpected lookup: %s %s", network, host)
		}
		return []netip.Addr{netip.MustParseAddr(addresses[host])}, nil
	}
	var connections int
	dial := func(_ context.Context, _, address string) (net.Conn, error) {
		connections++
		var host, reply string
		switch address {
		case "8.8.8.8:80":
			host, reply = "jobs.company.com", "302 Found\r\nLocation: http://careers.company.com/open#section"
		case "1.1.1.1:80":
			host, reply = "careers.company.com", "405 Method Not Allowed"
		default:
			t.Fatalf("dial was not pinned: %s", address)
		}
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			req, err := http.ReadRequest(bufio.NewReader(server))
			if err != nil {
				t.Error(err)
				return
			}
			if req.Method != http.MethodHead || req.Host != host || req.Header.Get("Authorization") != "" {
				t.Errorf("unexpected probe: %s %s", req.Method, req.Host)
			}
			_, _ = io.WriteString(server, "HTTP/1.1 "+reply+"\r\nContent-Length: 0\r\n\r\n")
		}()
		return client, nil
	}
	got, err := preflight(context.Background(), "http://jobs.company.com/", []string{"jobs.company.com", "careers.company.com"}, lookup, dial)
	if err != nil || got != "http://careers.company.com/open" || connections != 2 {
		t.Fatalf("got %q, %v after %d connections", got, err, connections)
	}
}

func TestPreflightRejectsMixedDNSBeforeDialing(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "169.254.169.254", "100.64.0.1", "fc00::1", "::ffff:10.0.0.1", "64:ff9b::a00:1"} {
		t.Run(address, func(t *testing.T) {
			lookup := func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr(address)}, nil
			}
			dial := func(context.Context, string, string) (net.Conn, error) {
				t.Fatal("mixed public and non-public DNS must be rejected before dialing")
				return nil, nil
			}
			_, err := preflight(context.Background(), "http://jobs.company.com/", []string{"jobs.company.com"}, lookup, dial)
			if !errors.Is(err, ErrInput) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestPreflightRejectsForeignRedirectBeforeLookup(t *testing.T) {
	lookup := func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if host != "jobs.company.com" {
			t.Fatalf("unexpected redirect lookup: %s", host)
		}
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	dial := func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			if _, err := http.ReadRequest(bufio.NewReader(server)); err == nil {
				_, _ = io.WriteString(server, "HTTP/1.1 302 Found\r\nLocation: http://foreign.company.com/\r\nContent-Length: 0\r\n\r\n")
			}
		}()
		return client, nil
	}
	_, err := preflight(context.Background(), "http://jobs.company.com/", []string{"jobs.company.com"}, lookup, dial)
	if !errors.Is(err, ErrInput) {
		t.Fatalf("got %v", err)
	}
}
