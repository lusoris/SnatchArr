//go:build semgrep_fixture

package fixture

import (
	"net"
	"net/http/httptest"
)

// useClosedAddress is a sink for the closed-server fixture.
func useClosedAddress(string) {}

// useClosedNetAddress is a sink for the listener address fixture.
func useClosedNetAddress(net.Addr) {}

// closedServerURL documents a captured URL used after Close.
func closedServerURL() {
	// ruleid: test-closed-server-address-reuse
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()
	useClosedAddress(url)
}

// closedServerLiteral documents an address in a case literal after Close.
func closedServerLiteral() {
	// ruleid: test-closed-server-address-reuse
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()
	caseURL := struct{ baseURL string }{baseURL: url}
	_ = caseURL
}

// closedServerSliceLiteral documents the parallel subtest table shape.
func closedServerSliceLiteral() {
	// ruleid: test-closed-server-address-reuse
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()
	cases := []struct{ baseURL string }{{baseURL: url}}
	_ = cases
}

// closedServerListenerAddress documents the listener address form.
func closedServerListenerAddress() {
	// ruleid: test-closed-server-address-reuse
	srv := httptest.NewServer(nil)
	addr := srv.Listener.Addr().String()
	srv.Close()
	useClosedAddress(addr)
}

// closedServerNetAddress documents a net.Addr read from a closed listener.
func closedServerNetAddress() {
	// ruleid: test-closed-server-address-reuse
	srv := httptest.NewServer(nil)
	addr := srv.Listener.Addr()
	srv.Close()
	useClosedNetAddress(addr)
}

// closedServerDirectURL documents a direct URL read after Close.
func closedServerDirectURL() {
	// ruleid: test-closed-server-address-reuse
	srv := httptest.NewServer(nil)
	srv.Close()
	useClosedAddress(srv.URL)
}

// closedServerDirectNetAddress documents a direct net.Addr read after Close.
func closedServerDirectNetAddress() {
	// ruleid: test-closed-server-address-reuse
	srv := httptest.NewServer(nil)
	srv.Close()
	useClosedNetAddress(srv.Listener.Addr())
}

// closedServerDirectListenerAddress documents a direct string address read after Close.
func closedServerDirectListenerAddress() {
	// ruleid: test-closed-server-address-reuse
	srv := httptest.NewServer(nil)
	srv.Close()
	useClosedAddress(srv.Listener.Addr().String())
}

// closedTLSServerURL documents a closed TLS server address.
func closedTLSServerURL() {
	// ruleid: test-closed-server-address-reuse
	srv := httptest.NewTLSServer(nil)
	url := srv.URL
	srv.Close()
	useClosedAddress(url)
}

// liveServerURL documents a URL used before Close.
func liveServerURL() {
	srv := httptest.NewServer(nil)
	url := srv.URL
	// ok: test-closed-server-address-reuse
	useClosedAddress(url)
	srv.Close()
}

// liveServerDirectNetAddress documents a direct address read before Close.
func liveServerDirectNetAddress() {
	srv := httptest.NewServer(nil)
	// ok: test-closed-server-address-reuse
	useClosedNetAddress(srv.Listener.Addr())
	srv.Close()
}

// deferredServerClose documents a server kept live until return.
func deferredServerClose() {
	srv := httptest.NewServer(nil)
	defer srv.Close()
	url := srv.URL
	// ok: test-closed-server-address-reuse
	useClosedAddress(url)
}
