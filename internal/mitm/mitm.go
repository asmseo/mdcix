// Package mitm issues per-host TLS certificates signed by the local mdcix CA.
package mitm

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/asmseo/mdcix/internal/certs"
)

// Manager caches domain certificates signed by the local CA.
type Manager struct {
	caTLS *tls.Certificate
	caX509 *x509.Certificate
	caKey  *rsa.PrivateKey
	mu     sync.RWMutex
	cache  map[string]*tls.Certificate
	Dir    string
}

// New loads or creates the CA in dir.
func New(dir string) (*Manager, error) {
	ca, err := certs.LoadCA(dir)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(ca.Certificate[0])
	if err != nil {
		return nil, err
	}
	key, ok := ca.PrivateKey.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("CA key is not RSA")
	}
	return &Manager{
		caTLS:  &ca,
		caX509: leaf,
		caKey:  key,
		cache:  make(map[string]*tls.Certificate),
		Dir:    dir,
	}, nil
}

// GetCert returns a leaf cert for host (host:port accepted).
func (m *Manager) GetCert(hostport string) (*tls.Certificate, error) {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	m.mu.RLock()
	if c, ok := m.cache[host]; ok {
		m.mu.RUnlock()
		return c, nil
	}
	m.mu.RUnlock()

	cert, err := m.issue(host)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.cache[host] = cert
	m.mu.Unlock()
	return cert, nil
}

func (m *Manager) issue(host string) (*tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{host},
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, m.caX509, &key.PublicKey, m.caKey)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &tls.Certificate{
		Certificate: [][]byte{der, m.caTLS.Certificate[0]},
		PrivateKey:  key,
		Leaf:        leaf,
	}, nil
}

// CAPath is the PEM clients should trust.
func (m *Manager) CAPath() string {
	return certs.CACertPath(m.Dir)
}
