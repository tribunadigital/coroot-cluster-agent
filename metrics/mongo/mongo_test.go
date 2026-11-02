package mongo

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/coroot/coroot-cluster-agent/common"
	"github.com/coroot/logger"
)

type tlsFixture struct {
	caPEM        []byte
	caCert       *x509.Certificate
	caKey        *ecdsa.PrivateKey
	clientPEM    []byte
	clientKeyPEM []byte
}

func newTLSFixture(t *testing.T) tlsFixture {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	f := tlsFixture{
		caPEM:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		caCert: caCert,
		caKey:  caKey,
	}

	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "test-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	f.clientPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientDER})
	f.clientKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: mustMarshalECKey(t, clientKey)})

	return f
}

// serverCert returns a leaf certificate signed by the fixture CA with the
// given SANs. Tests choose SANs explicitly: the chain-only cases must use a
// certificate without the dialed IP to prove that no hostname/IP verification
// happens on the client.
func (f tlsFixture) serverCert(t *testing.T, dnsNames []string, ips []net.IP) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "mongo.test"},
		DNSNames:     dnsNames,
		IPAddresses:  ips,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, f.caCert, &key.PublicKey, f.caKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: mustMarshalECKey(t, key)})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func mustMarshalECKey(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// tlsListener is a cleanup-safe TLS server fixture: accepted connections are
// tracked and closed on test cleanup, handshakes are deadline-bounded, and
// result reporting never blocks a goroutine after the test has finished.
type tlsListener struct {
	addr    string
	stateCh chan tls.ConnectionState
	errCh   chan error

	done  chan struct{}
	close sync.Once
	mu    sync.Mutex
	conns []net.Conn
}

func startTLSListener(t *testing.T, serverCfg *tls.Config) *tlsListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := &tlsListener{
		addr:    ln.Addr().String(),
		stateCh: make(chan tls.ConnectionState, 8),
		errCh:   make(chan error, 8),
		done:    make(chan struct{}),
	}
	t.Cleanup(func() {
		l.close.Do(func() { close(l.done) })
		_ = ln.Close()
		l.mu.Lock()
		for _, c := range l.conns {
			_ = c.Close()
		}
		l.mu.Unlock()
	})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			l.mu.Lock()
			l.conns = append(l.conns, conn)
			l.mu.Unlock()
			go l.serve(conn, serverCfg)
		}
	}()
	return l
}

func (l *tlsListener) serve(conn net.Conn, serverCfg *tls.Config) {
	tc := tls.Server(conn, serverCfg)
	// Bound the handshake lifetime; cleanup closing the conn also unblocks it.
	_ = tc.SetDeadline(time.Now().Add(10 * time.Second))
	if err := tc.Handshake(); err != nil {
		select {
		case l.errCh <- err:
		default:
		}
		return
	}
	select {
	case l.stateCh <- tc.ConnectionState():
	case <-l.done:
	}
}

func TestNewInvalidTLSParam(t *testing.T) {
	c, err := New("127.0.0.1:27017", "", "", "",
		common.TLSCredentials{}, map[string]string{"tls": "banana"},
		time.Minute, 10*time.Second, logger.NewKlog("test"), nil, "test", 0, false)
	if err == nil {
		t.Fatal("expected an error for an invalid tls param")
	}
	if c != nil {
		t.Error("collector must be nil on error")
	}
}

func TestNewInvalidCAFailsClosed(t *testing.T) {
	c, err := New("127.0.0.1:27017", "", "", "",
		common.TLSCredentials{CA: "not a pem"}, map[string]string{"tls": "true"},
		time.Minute, 10*time.Second, logger.NewKlog("test"), nil, "test", 0, false)
	if err == nil {
		t.Fatal("expected an error for an invalid CA")
	}
	if c != nil {
		t.Error("collector must be nil on error: no plaintext fallback allowed")
	}
}

func TestNewInvalidClientCertFailsClosed(t *testing.T) {
	f := newTLSFixture(t)
	badKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	badKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: mustMarshalECKey(t, badKey)})

	c, err := New("127.0.0.1:27017", "", "", "",
		common.TLSCredentials{CA: string(f.caPEM), Cert: string(f.clientPEM), Key: string(badKeyPEM)},
		map[string]string{"tls": "true"},
		time.Minute, 10*time.Second, logger.NewKlog("test"), nil, "test", 0, false)
	if err == nil {
		t.Fatal("expected an error for an invalid client certificate")
	}
	if c != nil {
		t.Error("collector must be nil on error: no plaintext fallback allowed")
	}
}

func TestNewWithoutTLSSucceeds(t *testing.T) {
	c, err := New("127.0.0.1:27017", "", "", "",
		common.TLSCredentials{}, nil,
		time.Hour, 10*time.Second, logger.NewKlog("test"), nil, "test", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("collector must not be nil")
	}
	_ = c.Close()
}

// The collector must connect over TLS, send the configured SNI, and trust
// only the configured CA. The MongoDB wire protocol never needs to succeed:
// the TLS handshake itself is what this test verifies.
func TestNewTLSHandshakeSendsConfiguredSNI(t *testing.T) {
	f := newTLSFixture(t)
	serverCfg := &tls.Config{Certificates: []tls.Certificate{f.serverCert(t, []string{"mongo.test"}, nil)}}
	l := startTLSListener(t, serverCfg)

	c, err := New(l.addr, "", "", "mongo.test",
		common.TLSCredentials{CA: string(f.caPEM)}, map[string]string{"tls": "true"},
		time.Hour, 5*time.Second, logger.NewKlog("test"), nil, "test", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = c.Close() }()

	select {
	case st := <-l.stateCh:
		if st.ServerName != "mongo.test" {
			t.Errorf("server saw SNI %q, want %q", st.ServerName, "mongo.test")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the TLS handshake")
	}
}

// With no SNI configured (pod-discovered targets are scraped by IP), the
// handshake must still succeed via chain-only verification. The server
// certificate intentionally has no SAN for the dialed IP (127.0.0.1): any
// hostname/IP verification on the client would fail this handshake, so
// success proves verification is chain-only.
func TestNewTLSHandshakeWithoutSNIVerifiesChainOnly(t *testing.T) {
	f := newTLSFixture(t)
	serverCfg := &tls.Config{Certificates: []tls.Certificate{f.serverCert(t, []string{"mongo.test"}, nil)}}
	l := startTLSListener(t, serverCfg)

	c, err := New(l.addr, "", "", "",
		common.TLSCredentials{CA: string(f.caPEM)}, map[string]string{"tls": "true"},
		time.Hour, 5*time.Second, logger.NewKlog("test"), nil, "test", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = c.Close() }()

	select {
	case st := <-l.stateCh:
		if st.ServerName != "" {
			t.Errorf("server saw SNI %q, want empty (no SNI must be synthesized)", st.ServerName)
		}
		// Reaching this point means the client completed the handshake,
		// i.e. its chain-only VerifyConnection accepted the server chain.
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the TLS handshake")
	}
}

// A server certificate signed by an untrusted CA must be rejected: the
// constructor wiring must pass the configured CA into the TLS config, not
// fall back to the system pool or skip verification.
func TestNewTLSHandshakeRejectsUntrustedCA(t *testing.T) {
	server := newTLSFixture(t)
	untrusted := newTLSFixture(t)
	serverCfg := &tls.Config{Certificates: []tls.Certificate{server.serverCert(t, []string{"mongo.test"}, nil)}}
	l := startTLSListener(t, serverCfg)

	c, err := New(l.addr, "", "", "",
		common.TLSCredentials{CA: string(untrusted.caPEM)}, map[string]string{"tls": "true"},
		time.Hour, 5*time.Second, logger.NewKlog("test"), nil, "test", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = c.Close() }()

	select {
	case st := <-l.stateCh:
		t.Fatalf("handshake with an untrusted CA must fail, got state %+v", st)
	case <-l.errCh:
		// The client aborted the handshake: the chain was rejected.
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the rejected handshake")
	}
}

// With tls=skip-verify the constructor must wire InsecureSkipVerify through:
// a certificate from an unknown CA is accepted.
func TestNewTLSHandshakeSkipVerify(t *testing.T) {
	f := newTLSFixture(t)
	serverCfg := &tls.Config{Certificates: []tls.Certificate{f.serverCert(t, []string{"mongo.test"}, nil)}}
	l := startTLSListener(t, serverCfg)

	c, err := New(l.addr, "", "", "",
		common.TLSCredentials{}, map[string]string{"tls": "skip-verify"},
		time.Hour, 5*time.Second, logger.NewKlog("test"), nil, "test", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = c.Close() }()

	select {
	case <-l.stateCh:
		// Handshake succeeded despite the unknown CA: skip-verify is wired.
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the TLS handshake")
	}
}
