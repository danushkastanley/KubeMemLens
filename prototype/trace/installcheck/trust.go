package installcheck

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"time"
)

// TrustMaterial contains administrator-supplied TLS material already mounted
// into the admission API. Node material contains public certificates only.
type TrustMaterial struct {
	APICertificate, APIKey         []byte
	ControlCertificate, ControlKey []byte
	NodeCA                         []byte
	Nodes                          map[string]NodeTrust
}

type NodeTrust struct {
	Certificate, ControlCA []byte
}

func (TrustMaterial) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private installation trust]")
}
func (NodeTrust) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[private node trust]") }

type certificateChain struct {
	leaf          *x509.Certificate
	intermediates *x509.CertPool
}

func CheckTrust(spec Spec, material TrustMaterial, now time.Time) error {
	if spec.Validate() != nil || len(material.Nodes) != len(spec.Nodes) || now.IsZero() {
		return ErrConfiguration
	}
	api, err := identity(material.APICertificate, material.APIKey)
	if err != nil || verify(api, spec.APICABundle, spec.APIServiceName+"."+spec.Namespace+".svc", x509.ExtKeyUsageServerAuth, now) != nil {
		return ErrTrust
	}
	control, err := identity(material.ControlCertificate, material.ControlKey)
	if err != nil || digest(control.leaf.Raw) != spec.ControlCertificateSHA256 {
		return ErrTrust
	}
	for _, node := range spec.Nodes {
		trust, found := material.Nodes[node.ID]
		if !found {
			return ErrTrust
		}
		certificate, err := certificate(trust.Certificate)
		if err != nil || digest(certificate.leaf.Raw) != node.CertificateSHA256 {
			return ErrTrust
		}
		host := spec.NodeServicePrefix + "-" + node.ID + "." + spec.Namespace + ".svc"
		if verify(certificate, material.NodeCA, host, x509.ExtKeyUsageServerAuth, now) != nil || verify(control, trust.ControlCA, "", x509.ExtKeyUsageClientAuth, now) != nil {
			return ErrTrust
		}
	}
	return nil
}

func identity(certificatePEM, keyPEM []byte) (certificateChain, error) {
	if len(certificatePEM) > 64<<10 || len(keyPEM) > 64<<10 {
		return certificateChain{}, ErrTrust
	}
	identity, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		return certificateChain{}, ErrTrust
	}
	return chain(identity.Certificate)
}

func certificate(data []byte) (certificateChain, error) {
	if len(data) > 64<<10 {
		return certificateChain{}, ErrTrust
	}
	var certificates [][]byte
	for len(bytes.TrimSpace(data)) > 0 {
		block, rest := pem.Decode(data)
		if block == nil || block.Type != "CERTIFICATE" || len(certificates) == 8 {
			return certificateChain{}, ErrTrust
		}
		certificates = append(certificates, block.Bytes)
		data = rest
	}
	return chain(certificates)
}

func chain(certificates [][]byte) (certificateChain, error) {
	if len(certificates) == 0 || len(certificates) > 8 {
		return certificateChain{}, ErrTrust
	}
	result := certificateChain{intermediates: x509.NewCertPool()}
	for index, raw := range certificates {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			return certificateChain{}, ErrTrust
		}
		if index == 0 {
			result.leaf = cert
			continue
		}
		result.intermediates.AddCert(cert)
	}
	return result, nil
}

func verify(cert certificateChain, ca []byte, host string, usage x509.ExtKeyUsage, now time.Time) error {
	if len(ca) == 0 || len(ca) > 64<<10 || cert.leaf.IsCA {
		return ErrTrust
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return ErrTrust
	}
	_, err := cert.leaf.Verify(x509.VerifyOptions{Roots: pool, Intermediates: cert.intermediates, DNSName: host, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{usage}})
	return err
}

func digest(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
