package firecrawl

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

var excludedNetworks = [...]netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

var ErrPreflightDNS = fmt.Errorf("source DNS lookup failed: %w", ErrNetwork)
var ErrPreflightConnection = fmt.Errorf("could not establish source connection: %w", ErrNetwork)

var globalIPv6 = netip.MustParsePrefix("2000::/3")

// Preflight resolves and checks each redirect before a scrape is reserved.
// It makes only unauthenticated HEAD requests to the approved source hosts.
func Preflight(ctx context.Context, target string, allowedHosts []string) (string, error) {
	dialer := &net.Dialer{}
	return preflight(ctx, target, allowedHosts, net.DefaultResolver.LookupNetIP, dialer.DialContext)
}

type lookupIP func(context.Context, string, string) ([]netip.Addr, error)
type dialIP func(context.Context, string, string) (net.Conn, error)

func preflight(ctx context.Context, target string, allowedHosts []string, lookup lookupIP, dial dialIP) (string, error) {
	for hop := 0; hop < 4; hop++ {
		if !allowedURL(target, allowedHosts) {
			return "", ErrInput
		}
		current, _ := url.Parse(target)
		current.Fragment = ""
		result, err := probeTarget(ctx, current, lookup, dial)
		if err != nil {
			return "", err
		}
		if result == "" {
			return current.String(), nil
		}
		next, err := current.Parse(result)
		if err != nil {
			return "", ErrTarget
		}
		target = next.String()
	}
	return "", ErrTarget
}

func probeTarget(ctx context.Context, target *url.URL, lookup lookupIP, dial dialIP) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	addresses, err := lookup(ctx, "ip", target.Hostname())
	if err != nil || len(addresses) == 0 {
		return "", ErrPreflightDNS
	}
	for _, address := range addresses {
		if !publicAddress(address.Unmap()) {
			return "", ErrInput
		}
	}

	// Use the validated addresses directly and keep the hostname for HTTP and TLS.
	transport := &http.Transport{
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 32 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			for _, ip := range addresses {
				conn, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
			}
			return nil, ErrPreflightConnection
		},
	}
	defer transport.CloseIdleConnections()
	client := http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target.String(), nil)
	if err != nil {
		return "", ErrInput
	}
	reply, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrPreflightConnection) {
			return "", ErrPreflightConnection
		}
		return "", ErrNetwork
	}
	defer reply.Body.Close()
	status := reply.StatusCode
	if status >= 300 && status < 400 {
		if location := reply.Header.Get("Location"); location != "" {
			return location, nil
		}
		return "", ErrTarget
	}
	if status == http.StatusMethodNotAllowed || (status >= 200 && status < 300) {
		return "", nil
	}
	return "", ErrTarget
}

func publicAddress(address netip.Addr) bool {
	if !address.IsGlobalUnicast() || address.IsPrivate() ||
		(address.Is6() && !globalIPv6.Contains(address)) {
		return false
	}
	for _, prefix := range excludedNetworks {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
