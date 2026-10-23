// Package realip derives the real client address, scheme and host of a request.
//
// Forwarding headers (X-Forwarded-For/-Proto/-Host) are believed only when the direct
// peer is a configured trusted proxy; otherwise anyone could spoof their IP or make the
// site generate links for another host (Blessing Skin trusted these headers from
// everyone — defect S01). The default trusts nobody.
package realip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Info is the resolved view of a request.
type Info struct {
	ClientIP        string // best-known client address, never empty for a real request
	Proto           string // "http" or "https"
	Host            string // public host[:port] the client asked for
	ViaTrustedProxy bool   // the direct peer is a trusted proxy
}

// Resolver applies a trusted proxy list. It is immutable; build a new one to change it.
type Resolver struct {
	trusted []netip.Prefix
}

// New parses CIDRs or plain IPs ("10.0.0.0/8", "127.0.0.1", "fd00::/8").
func New(trusted []string) (*Resolver, error) {
	r := &Resolver{}
	for _, s := range trusted {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			addr, aerr := netip.ParseAddr(s)
			if aerr != nil {
				return nil, fmt.Errorf("trusted proxy %q is not an IP or CIDR", s)
			}
			p = netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen())
		}
		r.trusted = append(r.trusted, p.Masked())
	}
	return r, nil
}

// Trusts reports whether addr is a trusted proxy.
func (r *Resolver) Trusts(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, p := range r.trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// Resolve inspects r's peer address and, for trusted peers, its forwarding headers.
func (r *Resolver) Resolve(req *http.Request) Info {
	info := Info{Proto: "http", Host: req.Host}
	if req.TLS != nil {
		info.Proto = "https"
	}
	peer, ok := parseHostPort(req.RemoteAddr)
	if !ok {
		info.ClientIP = req.RemoteAddr
		return info
	}
	info.ClientIP = peer.String()
	if !r.Trusts(peer) {
		return info
	}
	info.ViaTrustedProxy = true

	// Walk X-Forwarded-For from the right: the first address that is not one of our
	// proxies is the client. Left-most entries are whatever the client claimed.
	hops := forwardedFor(req.Header.Values("X-Forwarded-For"))
	for i := len(hops) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(hops[i])
		if err != nil {
			break // garbage from here on cannot be trusted
		}
		info.ClientIP = addr.Unmap().String()
		if !r.Trusts(addr) {
			break
		}
	}
	if p := strings.ToLower(firstValue(req.Header.Get("X-Forwarded-Proto"))); p == "http" || p == "https" {
		info.Proto = p
	}
	if h := firstValue(req.Header.Get("X-Forwarded-Host")); validHost(h) {
		info.Host = h
	}
	return info
}

func parseHostPort(s string) (netip.Addr, bool) {
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap(), true
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Unmap(), true
	}
	return netip.Addr{}, false
}

func forwardedFor(values []string) []string {
	var out []string
	for _, v := range values {
		for part := range strings.SplitSeq(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// firstValue takes the first entry of a comma-separated header (the outermost proxy
// writes first when proxies append).
func firstValue(v string) string {
	first, _, _ := strings.Cut(v, ",")
	return strings.TrimSpace(first)
}

// validHost accepts host or host:port made of hostname/IP characters only.
func validHost(h string) bool {
	if h == "" || len(h) > 255 {
		return false
	}
	host := h
	if strings.HasPrefix(h, "[") { // [ipv6]:port
		end := strings.IndexByte(h, ']')
		if end < 0 {
			return false
		}
		if _, err := netip.ParseAddr(h[1:end]); err != nil {
			return false
		}
		return end == len(h)-1 || (h[end+1] == ':' && isDigits(h[end+2:]))
	}
	if hh, port, err := net.SplitHostPort(h); err == nil {
		if !isDigits(port) {
			return false
		}
		host = hh
	}
	for _, c := range host {
		if !(c == '.' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	if s == "" || len(s) > 5 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
