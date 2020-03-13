package corehttp

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	cid "github.com/ipfs/go-cid"
	core "github.com/ipfs/go-ipfs/core"
	namesys "github.com/ipfs/go-ipfs/namesys"
	mbase "github.com/multiformats/go-multibase"

	config "github.com/ipfs/go-ipfs-config"
	nsopts "github.com/ipfs/interface-go-ipfs-core/options/namesys"
	isd "github.com/jbenet/go-is-domain"
)

var defaultPaths = []string{"/ipfs/", "/ipns/", "/api/", "/p2p/", "/version"}

var pathGatewaySpec = config.GatewaySpec{
	Paths:         defaultPaths,
	UseSubdomains: false,
}

var subdomainGatewaySpec = config.GatewaySpec{
	Paths:         defaultPaths,
	UseSubdomains: true,
}

var defaultKnownGateways = map[string]config.GatewaySpec{
	"localhost":       subdomainGatewaySpec,
	"ipfs.io":         pathGatewaySpec,
	"gateway.ipfs.io": pathGatewaySpec,
	"dweb.link":       subdomainGatewaySpec,
}

// Find content identifier, protocol, and remaining hostname (host+optional port)
// of a subdomain gateway (eg. *.ipfs.foo.bar.co.uk)
var subdomainGatewayRegex = regexp.MustCompile(`^(.+)\.(ipfs|ipns|ipld|p2p)\.([^/?#&]+)$`)

// HostnameOption rewrites an incoming request based on the Host header.
func HostnameOption() ServeOption {
	return func(n *core.IpfsNode, _ net.Listener, mux *http.ServeMux) (*http.ServeMux, error) {
		childMux := http.NewServeMux()

		cfg, err := n.Repo.Config()
		if err != nil {
			return nil, err
		}
		knownGateways := make(
			map[string]config.GatewaySpec,
			len(defaultKnownGateways)+len(cfg.Gateway.PublicGateways),
		)
		for hostname, gw := range defaultKnownGateways {
			knownGateways[hostname] = gw
		}
		for hostname, gw := range cfg.Gateway.PublicGateways {
			if gw == nil {
				// Allows the user to remove gateways but _also_
				// allows us to continuously update the list.
				delete(knownGateways, hostname)
			} else {
				knownGateways[hostname] = *gw
			}
		}

		// Return matching GatewaySpec with gracefull fallback to version without port
		isKnownGateway := func(hostname string) (gw config.GatewaySpec, ok bool) {
			// Try hostname (host+optional port - value from Host header as-is)
			if gw, ok := knownGateways[hostname]; ok {
				return gw, ok
			}
			// Fallback to hostname without port
			gw, ok = knownGateways[stripPort(hostname)]
			return gw, ok
		}

		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// Unfortunately, many (well, ipfs.io) gateways use
			// DNSLink so if we blindly rewrite with DNSLink, we'll
			// break /ipfs links.
			//
			// We fix this by maintaining a list of known gateways
			// and the paths that they serve "gateway" content on.
			// That way, we can use DNSLink for everything else.

			// HTTP Host & Path check: is this one of our  "known gateways"?
			if gw, ok := isKnownGateway(r.Host); ok {
				// This is a known gateway but request is not using
				// the subdomain feature.

				// Does this gateway _handle_ this path?
				if hasPrefix(r.URL.Path, gw.Paths...) {
					// It does.

					// Should this gateway use subdomains instead of paths?
					if gw.UseSubdomains {
						// Yes, redirect if applicable
						// Example: dweb.link/ipfs/{cid} → {cid}.ipfs.dweb.link
						if newURL, ok := toSubdomainURL(r.Host, r.URL.Path, r); ok {
							http.Redirect(w, r, newURL, http.StatusMovedPermanently)
							return
						}
					}

					// Not a subdomain resource, continue with path processing
					// Example: 127.0.0.1:8080/ipfs/{CID}, ipfs.io/ipfs/{CID} etc
					childMux.ServeHTTP(w, r)
					return
				}
				// Not a whitelisted path

				// Try DNSLink, if it was not explicitly disabled for the hostname
				if !gw.NoDNSLink && isDNSLinkRequest(r, n) {
					// rewrite path and handle as DNSLink
					r.URL.Path = "/ipns/" + stripPort(r.Host) + r.URL.Path
					childMux.ServeHTTP(w, r)
					return
				}

				// If not, resource does not exist on the hostname, return 404
				http.NotFound(w, r)
				return
			}

			// HTTP Host check: is this one of our subdomain-based "known gateways"?
			// Example: {cid}.ipfs.localhost, {cid}.ipfs.dweb.link
			if hostname, ns, rootID, ok := parseSubdomains(r.Host); ok {
				// Looks like we're using subdomains.

				// Again, is this a known gateway that supports subdomains?
				if gw, ok := isKnownGateway(hostname); ok {

					// Assemble original path prefix.
					pathPrefix := "/" + ns + "/" + rootID

					// Does this gateway _handle_ this path?
					if gw.UseSubdomains && hasPrefix(pathPrefix, gw.Paths...) {

						// Do we need to fix multicodec in /ipns/{cid}?
						if ns == "ipns" {
							keyCid, err := cid.Decode(rootID)
							if err == nil && keyCid.Type() != cid.Libp2pKey {

								if newURL, ok := toSubdomainURL(hostname, pathPrefix+r.URL.Path, r); ok {
									// Redirect to CID fixed inside of toSubdomainURL()
									http.Redirect(w, r, newURL, http.StatusMovedPermanently)
									return
								}
							}
						}

						// Rewrite the path to not use subdomains
						r.URL.Path = pathPrefix + r.URL.Path
						// Serve path request
						childMux.ServeHTTP(w, r)
						return
					}
					// If not, resource does not exist on the subdomain gateway, return 404
					http.NotFound(w, r)
					return
				}
			}
			// We don't have a known gateway. Fallback on DNSLink lookup

			// Wildcard HTTP Host check:
			// 1. is wildcard DNSLink enabled (Gateway.NoDNSLink=false)?
			// 2. does Host header include a fully qualified domain name (FQDN)?
			// 3. does DNSLink record exist in DNS?
			if !cfg.Gateway.NoDNSLink && isDNSLinkRequest(r, n) {
				// rewrite path and handle as DNSLink
				r.URL.Path = "/ipns/" + stripPort(r.Host) + r.URL.Path
				childMux.ServeHTTP(w, r)
				return
			}

			// else, treat it as an old school gateway, I guess.
			childMux.ServeHTTP(w, r)
		})
		return childMux, nil
	}
}

// isDNSLinkRequest returns bool that indicates if request
// should return data from content path listed in DNSLink record (if exists)
func isDNSLinkRequest(r *http.Request, n *core.IpfsNode) bool {
	fqdn := stripPort(r.Host)
	if len(fqdn) == 0 && !isd.IsDomain(fqdn) {
		return false
	}
	name := "/ipns/" + fqdn
	// check if DNSLink exists
	_, err := n.Namesys.Resolve(n.Context(), name, nsopts.Depth(1))
	return err == nil || err == namesys.ErrResolveRecursion
}

func isSubdomainNamespace(ns string) bool {
	switch ns {
	case "ipfs", "ipns", "p2p", "ipld":
		return true
	default:
		return false
	}
}

func isPeerIDNamespace(ns string) bool {
	switch ns {
	case "ipns", "p2p":
		return true
	default:
		return false
	}
}

// Parses Host header of a subdomain-based URL and returns it's components
// Note: hostname is host + optional port
func parseSubdomains(hostHeader string) (hostname, ns, rootID string, ok bool) {
	parts := subdomainGatewayRegex.FindStringSubmatch(hostHeader)
	if len(parts) < 4 || !isSubdomainNamespace(parts[2]) {
		return "", "", "", false
	}
	return parts[3], parts[2], parts[1], true
}

// Converts a hostname/path to a subdomain-based URL, if applicable.
func toSubdomainURL(hostname, path string, r *http.Request) (redirURL string, ok bool) {
	var scheme, ns, rootID, rest string

	query := r.URL.RawQuery
	parts := strings.SplitN(path, "/", 4)
	safeRedirectURL := func(in string) (out string, ok bool) {
		safeURI, err := url.ParseRequestURI(in)
		if err != nil {
			return "", false
		}
		return safeURI.String(), true
	}

	// Support X-Forwarded-Proto if added by a reverse proxy
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/X-Forwarded-Proto
	xproto := r.Header.Get("X-Forwarded-Proto")
	if xproto == "https" {
		scheme = "https:"
	} else {
		scheme = "http:"
	}

	switch len(parts) {
	case 4:
		rest = parts[3]
		fallthrough
	case 3:
		ns = parts[1]
		rootID = parts[2]
	default:
		return "", false
	}

	if !isSubdomainNamespace(ns) {
		return "", false
	}

	// add prefix if query is present
	if query != "" {
		query = "?" + query
	}

	if rootCid, err := cid.Decode(rootID); err == nil {
		multicodec := rootCid.Type()

		// PeerIDs represented as CIDv1 are expected to have libp2p-key
		// multicodec (https://github.com/libp2p/specs/pull/209).
		// We ease the transition by fixing multicodec on the fly:
		// https://github.com/ipfs/go-ipfs/issues/5287#issuecomment-492163929
		if isPeerIDNamespace(ns) && multicodec != cid.Libp2pKey {
			multicodec = cid.Libp2pKey
		}

		// if object turns out to be a valid CID,
		// ensure text representation used in subdomain is CIDv1 in Base32
		// https://github.com/ipfs/in-web-browsers/issues/89
		rootID, err = cid.NewCidV1(multicodec, rootCid.Hash()).StringOfBase(mbase.Base32)
		if err != nil {
			// should not error, but if it does, its clealy not possible to
			// produce a subdomain URL
			return "", false
		}
	}

	return safeRedirectURL(fmt.Sprintf(
		"%s//%s.%s.%s/%s%s",
		scheme,
		rootID,
		ns,
		hostname,
		rest,
		query,
	))
}

func hasPrefix(path string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		// Assume people are creative with trailing slashes in Gateway config
		p := strings.TrimSuffix(prefix, "/")
		// Support for both /version and /ipfs/$cid
		if p == path || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

func stripPort(hostname string) string {
	host, _, err := net.SplitHostPort(hostname)
	if err == nil {
		return host
	}
	// noop: this should never happen
	// at this stage of request processing
	return hostname
}
