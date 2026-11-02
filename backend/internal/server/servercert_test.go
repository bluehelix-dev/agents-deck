package server

import (
	"crypto/x509"
	"path/filepath"
	"slices"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/pki"
)

// The listener's certificate follows the computer's addresses: an address
// that appears (a VPN coming up) means a new certificate that covers it,
// served from then on; one that goes away changes nothing.
func TestServerCertFollowsAddresses(t *testing.T) {
	dir := t.TempDir()
	ca, err := pki.LoadOrCreateCA(filepath.Join(dir, "ca.pem"), pki.FileKeystore{Dir: filepath.Join(dir, "keys")}, "test")
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(dir, "server-cert.pem"), filepath.Join(dir, "server-key.pem")
	lan := []string{"deck.local", "localhost", "127.0.0.1", "192.0.2.10"}
	boards := []string{"deck.local", "192.0.2.10"}
	first, err := ca.ServerCert(certPath, keyPath, lan)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{CA: ca}
	s.SetServerCert(first, boards)
	cfg := s.TLSConfig(first)
	fp1 := pki.Fingerprint(first.Certificate[0])
	if s.ServerFP() != fp1 || !slices.Equal(s.Hosts(), boards) {
		t.Fatalf("installed: %s %v", s.ServerFP(), s.Hosts())
	}

	// The same addresses: the same certificate.
	s.RefreshServerCert(certPath, keyPath, lan, boards, t.Logf)
	if s.ServerFP() != fp1 {
		t.Fatal("reissued for the same addresses")
	}

	// A VPN address appears: a new certificate that names it, and the
	// listener serves it from now on.
	vpn := append(slices.Clone(lan), "198.51.100.7")
	vpnBoards := append(slices.Clone(boards), "198.51.100.7")
	s.RefreshServerCert(certPath, keyPath, vpn, vpnBoards, t.Logf)
	fp2 := s.ServerFP()
	if fp2 == fp1 {
		t.Fatal("not reissued for the new address")
	}
	perClient, err := cfg.GetConfigForClient(nil)
	if err != nil {
		t.Fatal(err)
	}
	served := perClient.Certificates[0]
	if pki.Fingerprint(served.Certificate[0]) != fp2 {
		t.Fatalf("served: %s, want %s", pki.Fingerprint(served.Certificate[0]), fp2)
	}
	leaf, err := x509.ParseCertificate(served.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if leaf.VerifyHostname("198.51.100.7") != nil || leaf.VerifyHostname("192.0.2.10") != nil {
		t.Fatalf("the new certificate covers %v %v", leaf.DNSNames, leaf.IPAddresses)
	}
	if !slices.Equal(s.Hosts(), vpnBoards) {
		t.Fatalf("hosts: %v", s.Hosts())
	}

	// The VPN goes away: the certificate stays, the hosts follow.
	s.RefreshServerCert(certPath, keyPath, lan, boards, t.Logf)
	if s.ServerFP() != fp2 || !slices.Equal(s.Hosts(), boards) {
		t.Fatalf("after the address went: %s %v", s.ServerFP(), s.Hosts())
	}
}
