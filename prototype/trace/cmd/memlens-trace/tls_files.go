package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/danushkastanley/kube-memlens/prototype/trace/nodebinding"
)

func readInstallationFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("installation file unavailable")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("installation file exceeds limit")
	}
	return data, nil
}
func loadIdentity(certFile, keyFile string) (tls.Certificate, error) {
	cert, err := readInstallationFile(certFile, 64<<10)
	if err != nil {
		return tls.Certificate{}, err
	}
	key, err := readInstallationFile(keyFile, 64<<10)
	if err != nil {
		return tls.Certificate{}, err
	}
	defer clear(key)
	identity, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return tls.Certificate{}, errors.New("invalid TLS identity")
	}
	return identity, nil
}
func loadPeer(caFile, digest string) (nodebinding.Peer, error) {
	ca, err := readInstallationFile(caFile, 64<<10)
	if err != nil {
		return nodebinding.Peer{}, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nodebinding.Peer{}, errors.New("invalid peer CA")
	}
	raw, err := hex.DecodeString(digest)
	if err != nil || len(raw) != 32 {
		return nodebinding.Peer{}, errors.New("invalid peer certificate digest")
	}
	var pin [32]byte
	copy(pin[:], raw)
	return nodebinding.Peer{Roots: pool, CertificateSHA256: pin}, nil
}

type endpointConfig struct {
	NodeUID           string `json:"nodeUID"`
	NodeName          string `json:"nodeName"`
	URL               string `json:"url"`
	CAFile            string `json:"caFile"`
	CertificateSHA256 string `json:"certificateSHA256"`
}

func loadEndpoints(path string) ([]nodebinding.Endpoint, error) {
	data, err := readInstallationFile(path, 64<<10)
	if err != nil {
		return nil, err
	}
	var configs []endpointConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&configs) != nil || len(configs) == 0 || len(configs) > 64 {
		return nil, errors.New("invalid node registry")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, errors.New("invalid node registry")
	}
	endpoints := make([]nodebinding.Endpoint, 0, len(configs))
	for _, c := range configs {
		peer, err := loadPeer(c.CAFile, c.CertificateSHA256)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, nodebinding.Endpoint{NodeUID: c.NodeUID, NodeName: c.NodeName, URL: c.URL, Peer: peer})
	}
	return endpoints, nil
}
