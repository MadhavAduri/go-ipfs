package corehttp

import (
	"testing"

	config "github.com/ipfs/go-ipfs-config"
)

func TestHasPrefix(t *testing.T) {
	for _, test := range []struct {
		prefixes []string
		path     string
		out      bool
	}{
		{[]string{"/ipfs"}, "/ipfs/cid", true},
		{[]string{"/ipfs/"}, "/ipfs/cid", true},
		{[]string{"/version/"}, "/version", true},
		{[]string{"/version"}, "/version", true},
	} {
		out := hasPrefix(test.path, test.prefixes...)
		if out != test.out {
			t.Errorf("(%+v, %s) returned '%t', expected '%t'", test.prefixes, test.path, out, test.out)
		}
	}
}

func TestPortStripping(t *testing.T) {
	for _, test := range []struct {
		in  string
		out string
	}{
		{"localhost:8080", "localhost"},
		{"bafkreicysg23kiwv34eg2d7qweipxwosdo2py4ldv42nbauguluen5v6am.ipfs.localhost:8080", "bafkreicysg23kiwv34eg2d7qweipxwosdo2py4ldv42nbauguluen5v6am.ipfs.localhost"},
		{"example.com:443", "example.com"},
		{"example.com", "example.com"},
		{"foo-dweb.ipfs.pvt.k12.ma.us:8080", "foo-dweb.ipfs.pvt.k12.ma.us"},
		{"localhost", "localhost"},
		{"[::1]:8080", "::1"},
	} {
		out := stripPort(test.in)
		if out != test.out {
			t.Errorf("(%s): returned '%s', expected '%s'", test.in, out, test.out)
		}
	}

}

func TestKnownSubdomainDetails(t *testing.T) {
	gwSpec := config.GatewaySpec{
		UseSubdomains: true,
	}
	knownGateways := map[string]config.GatewaySpec{
		"localhost":               gwSpec,
		"dweb.link":               gwSpec,
		"dweb.ipfs.pvt.k12.ma.us": gwSpec, // note the sneaky ".ipfs." ;-)
	}

	for _, test := range []struct {
		// in:
		hostHeader string
		// out:
		hostname string
		ns       string
		rootID   string
		ok       bool
	}{
		// no subdomain
		{"127.0.0.1:8080", "", "", "", false},
		{"[::1]:8080", "", "", "", false},
		{"hey.look.example.com", "", "", "", false},
		{"dweb.link", "", "", "", false},
		// malformed Host header
		{".....dweb.link", "", "", "", false},
		{"link", "", "", "", false},
		{"8080:dweb.link", "", "", "", false},
		{" ", "", "", "", false},
		{"", "", "", "", false},
		// unknown gateway host
		{"bafkreicysg23kiwv34eg2d7qweipxwosdo2py4ldv42nbauguluen5v6am.ipfs.unknown.example.com", "", "", "", false},
		// cid in subdomain, known gateway
		{"bafkreicysg23kiwv34eg2d7qweipxwosdo2py4ldv42nbauguluen5v6am.ipfs.localhost:8080", "localhost:8080", "ipfs", "bafkreicysg23kiwv34eg2d7qweipxwosdo2py4ldv42nbauguluen5v6am", true},
		{"bafkreicysg23kiwv34eg2d7qweipxwosdo2py4ldv42nbauguluen5v6am.ipfs.dweb.link", "dweb.link", "ipfs", "bafkreicysg23kiwv34eg2d7qweipxwosdo2py4ldv42nbauguluen5v6am", true},
		// capture everything before .ipfs.
		{"foo.bar.boo-buzz.ipfs.dweb.link", "dweb.link", "ipfs", "foo.bar.boo-buzz", true},
		// ipns
		{"bafzbeihe35nmjqar22thmxsnlsgxppd66pseq6tscs4mo25y55juhh6bju.ipns.localhost:8080", "localhost:8080", "ipns", "bafzbeihe35nmjqar22thmxsnlsgxppd66pseq6tscs4mo25y55juhh6bju", true},
		{"bafzbeihe35nmjqar22thmxsnlsgxppd66pseq6tscs4mo25y55juhh6bju.ipns.dweb.link", "dweb.link", "ipns", "bafzbeihe35nmjqar22thmxsnlsgxppd66pseq6tscs4mo25y55juhh6bju", true},
		// edge case check: public gateway under long TLD (see: https://publicsuffix.org)
		{"bafkreicysg23kiwv34eg2d7qweipxwosdo2py4ldv42nbauguluen5v6am.ipfs.dweb.ipfs.pvt.k12.ma.us", "dweb.ipfs.pvt.k12.ma.us", "ipfs", "bafkreicysg23kiwv34eg2d7qweipxwosdo2py4ldv42nbauguluen5v6am", true},
		{"bafzbeihe35nmjqar22thmxsnlsgxppd66pseq6tscs4mo25y55juhh6bju.ipns.dweb.ipfs.pvt.k12.ma.us", "dweb.ipfs.pvt.k12.ma.us", "ipns", "bafzbeihe35nmjqar22thmxsnlsgxppd66pseq6tscs4mo25y55juhh6bju", true},
		// dnslink in subdomain
		{"en.wikipedia-on-ipfs.org.ipns.localhost:8080", "localhost:8080", "ipns", "en.wikipedia-on-ipfs.org", true},
		{"en.wikipedia-on-ipfs.org.ipns.localhost", "localhost", "ipns", "en.wikipedia-on-ipfs.org", true},
		{"dist.ipfs.io.ipns.localhost:8080", "localhost:8080", "ipns", "dist.ipfs.io", true},
		{"en.wikipedia-on-ipfs.org.ipns.dweb.link", "dweb.link", "ipns", "en.wikipedia-on-ipfs.org", true},
		// edge case check: public gateway under long TLD (see: https://publicsuffix.org)
		{"foo.dweb.ipfs.pvt.k12.ma.us", "", "", "", false},
		{"bafkreicysg23kiwv34eg2d7qweipxwosdo2py4ldv42nbauguluen5v6am.ipfs.dweb.ipfs.pvt.k12.ma.us", "dweb.ipfs.pvt.k12.ma.us", "ipfs", "bafkreicysg23kiwv34eg2d7qweipxwosdo2py4ldv42nbauguluen5v6am", true},
		{"bafzbeihe35nmjqar22thmxsnlsgxppd66pseq6tscs4mo25y55juhh6bju.ipns.dweb.ipfs.pvt.k12.ma.us", "dweb.ipfs.pvt.k12.ma.us", "ipns", "bafzbeihe35nmjqar22thmxsnlsgxppd66pseq6tscs4mo25y55juhh6bju", true},
		// other namespaces
		{"api.localhost", "", "", "", false},
		{"peerid.p2p.localhost", "localhost", "p2p", "peerid", true},
	} {
		gw, hostname, ns, rootID, ok := knownSubdomainDetails(test.hostHeader, knownGateways)
		if ok != test.ok {
			t.Errorf("knownSubdomainDetails(%s): ok is %t, expected %t", test.hostHeader, ok, test.ok)
		}
		if rootID != test.rootID {
			t.Errorf("knownSubdomainDetails(%s): rootID is '%s', expected '%s'", test.hostHeader, rootID, test.rootID)
		}
		if ns != test.ns {
			t.Errorf("knownSubdomainDetails(%s): ns is '%s', expected '%s'", test.hostHeader, ns, test.ns)
		}
		if hostname != test.hostname {
			t.Errorf("knownSubdomainDetails(%s): hostname is '%s', expected '%s'", test.hostHeader, hostname, test.hostname)
		}
		if ok && gw.UseSubdomains != gwSpec.UseSubdomains {
			t.Errorf("knownSubdomainDetails(%s): gw is  %+v, expected %+v", test.hostHeader, gw, gwSpec)
		}
	}

}
