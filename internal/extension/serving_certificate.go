package extension

import (
	"crypto/x509"
	"encoding/pem"
	"io"
	"os"
)

// maxServingCertificateBytes bounds the read of the mounted certificate file.
const maxServingCertificateBytes = 64 << 10

// servingCertificateNotAfter reads the expiry of the first certificate in the
// mounted serving certificate file, as Unix seconds. The file is read on each
// call because the kubelet replaces it when the Secret is rotated. An
// unreadable or malformed file reports nothing rather than a guess.
func servingCertificateNotAfter(path string) *int64 {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxServingCertificateBytes))
	if err != nil {
		return nil
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}
	notAfter := certificate.NotAfter.Unix()
	return &notAfter
}
