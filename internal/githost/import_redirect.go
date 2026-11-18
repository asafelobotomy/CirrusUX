package githost

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// maxImportRedirects is how many redirects an import address may take.
const maxImportRedirects = 5

// followImport asks the address for its refs the way git would and follows
// its redirects itself, refusing any hop, and any connection, to an address
// inside this network. It returns where the repository really is. A server
// that can't be asked (it wants a password, say) keeps the address given:
// git then follows no redirect at all.
func (h *Host) followImport(ctx context.Context, u *url.URL) (*url.URL, error) {
	dialer := &net.Dialer{Timeout: 15 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		ap, err := netip.ParseAddrPort(address)
		if err != nil || h.inside(ap.Addr()) {
			return errInside
		}
		return nil
	}}
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext:     dialer.DialContext,
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: h.InsecureTLS}, //nolint:gosec // tests only
		},
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if r.URL.Scheme != "https" || len(via) > maxImportRedirects {
				return errf(Invalid, "That address redirects off https://, or too often")
			}
			return h.checkImportHost(r.Context(), r.URL.Hostname())
		},
	}
	probe := *u
	probe.Path = strings.TrimSuffix(u.Path, "/") + "/info/refs"
	probe.RawQuery = "service=git-upload-pack"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probe.String(), nil)
	if err != nil {
		return nil, errf(Invalid, "Only https:// addresses can be imported")
	}
	req.Header.Set("User-Agent", "git/2 (cirrusux import check)")
	resp, err := client.Do(req)
	if err != nil {
		if e := (*Error)(nil); errors.As(err, &e) {
			return nil, e // refused: a hop or a connection inside this network
		}
		return u, nil // unreachable or slow: git says why
	}
	resp.Body.Close()
	final := *resp.Request.URL
	base, ok := strings.CutSuffix(final.Path, "/info/refs")
	if !ok || final.Host == "" {
		return nil, errf(Invalid, "That address redirects somewhere that isn't a Git repository")
	}
	final.Path, final.RawPath, final.RawQuery, final.Fragment, final.User = base, "", "", "", nil
	return &final, nil
}
