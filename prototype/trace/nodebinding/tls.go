package nodebinding

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/url"
	"time"
)

// Peer is installation-owned trust: a CA plus an exact leaf certificate digest.
// Normal certificate verification remains mandatory before the pin is checked.
type Peer struct {
	Roots             *x509.CertPool
	CertificateSHA256 [32]byte
}

func (p Peer) valid() bool { return p.Roots != nil && p.CertificateSHA256 != ([32]byte{}) }
func (p Peer) verify(state tls.ConnectionState) error {
	if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 || sha256.Sum256(state.PeerCertificates[0].Raw) != p.CertificateSHA256 {
		return errors.New("node peer rejected")
	}
	return nil
}

func ServerTLS(identity tls.Certificate, control Peer) (*tls.Config, error) {
	if len(identity.Certificate) == 0 || !control.valid() {
		return nil, errors.New("invalid node TLS configuration")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{identity}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: control.Roots.Clone(), VerifyConnection: control.verify, SessionTicketsDisabled: true}, nil
}

// NewHTTPServer fixes transport limits and disables HTTP/2 to keep the private
// node listener's concurrency and per-connection memory bounds explicit.
func NewHTTPServer(address string, config *tls.Config, handler *Service) (*http.Server, error) {
	if config == nil || config.MinVersion < tls.VersionTLS13 || config.ClientAuth != tls.RequireAndVerifyClientCert || config.VerifyConnection == nil || handler == nil {
		return nil, errors.New("verified node TLS is required")
	}
	return &http.Server{Addr: address, TLSConfig: config.Clone(), Handler: handler, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 4096, TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){}}, nil
}

func newHTTPClient(endpoint string, identity tls.Certificate, node Peer) (*http.Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || len(identity.Certificate) == 0 || !node.valid() {
		return nil, errors.New("invalid node endpoint")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: node.Roots.Clone(), Certificates: []tls.Certificate{identity}, VerifyConnection: node.verify}, TLSHandshakeTimeout: 2 * time.Second, ResponseHeaderTimeout: 3 * time.Second, MaxResponseHeaderBytes: 4096, MaxConnsPerHost: 2, MaxIdleConnsPerHost: 2, IdleConnTimeout: 5 * time.Second, DisableCompression: true, ForceAttemptHTTP2: false}
	return &http.Client{Transport: transport, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}
