package certbootstrap

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/retry"
)

var apiServiceResource = schema.GroupVersionResource{
	Group: "apiregistration.k8s.io", Version: "v1", Resource: "apiservices",
}

type Options struct {
	Namespace     string
	SecretName    string
	APIService    string
	ServiceName   string
	Validity      time.Duration
	RotateBefore  time.Duration
	ForceRotation bool
	Now           func() time.Time
}

func Run(ctx context.Context, config *rest.Config, opts Options) error {
	if config == nil {
		return fmt.Errorf("Kubernetes config is required")
	}
	if opts.Namespace == "" || opts.SecretName == "" || opts.APIService == "" || opts.ServiceName == "" {
		return fmt.Errorf("namespace, Secret, APIService and Service names are required")
	}
	if opts.Validity <= 0 || opts.RotateBefore <= 0 || opts.RotateBefore >= opts.Validity {
		return fmt.Errorf("certificate validity and rotation window are invalid")
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create APIService client: %w", err)
	}
	secret, err := client.CoreV1().Secrets(opts.Namespace).Get(ctx, opts.SecretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get serving Secret: %w", err)
	}
	rotate := opts.ForceRotation || needsRotation(secret, opts, opts.Now())
	if !rotate {
		return reconcileCABundle(ctx, dynamicClient, opts.APIService, secret.Data["ca.crt"])
	}
	caPEM, certPEM, keyPEM, err := generate(opts, opts.Now())
	if err != nil {
		return err
	}
	oldCA := secret.Data["ca.crt"]
	if err := updateCABundle(ctx, dynamicClient, opts.APIService, append(append([]byte{}, oldCA...), caPEM...)); err != nil {
		return err
	}
	secret.Data = map[string][]byte{"ca.crt": caPEM, corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM}
	if _, err := client.CoreV1().Secrets(opts.Namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update serving Secret: %w", err)
	}
	if err := waitForServingCertificate(ctx, opts, caPEM); err != nil {
		return err
	}
	return updateCABundle(ctx, dynamicClient, opts.APIService, caPEM)
}

func needsRotation(secret *corev1.Secret, opts Options, now time.Time) bool {
	pair, err := tls.X509KeyPair(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
	if err != nil || len(pair.Certificate) == 0 {
		return true
	}
	certificate, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || !certificate.NotAfter.After(now.Add(opts.RotateBefore)) {
		return true
	}
	caCertificates, err := certutil.ParseCertsPEM(secret.Data["ca.crt"])
	if err != nil || len(caCertificates) == 0 {
		return true
	}
	roots := x509.NewCertPool()
	for _, ca := range caCertificates {
		if ca.IsCA && !now.Before(ca.NotBefore) && ca.NotAfter.After(now.Add(opts.RotateBefore)) {
			roots.AddCert(ca)
		}
	}
	if len(roots.Subjects()) == 0 {
		return true
	}
	_, err = certificate.Verify(x509.VerifyOptions{
		Roots: roots, DNSName: opts.ServiceName + "." + opts.Namespace + ".svc", CurrentTime: now,
	})
	return err != nil
}

func generate(opts Options, now time.Time) ([]byte, []byte, []byte, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate CA key: %w", err)
	}
	caTemplate, err := certificateTemplate("kube-memlens-extension", now, opts.Validity, true, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create CA certificate: %w", err)
	}
	servingKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate serving key: %w", err)
	}
	dnsNames := []string{
		opts.ServiceName,
		opts.ServiceName + "." + opts.Namespace,
		opts.ServiceName + "." + opts.Namespace + ".svc",
		opts.ServiceName + "." + opts.Namespace + ".svc.cluster.local",
	}
	servingTemplate, err := certificateTemplate(opts.ServiceName, now, opts.Validity, false, dnsNames)
	if err != nil {
		return nil, nil, nil, err
	}
	certDER, err := x509.CreateCertificate(rand.Reader, servingTemplate, caTemplate, &servingKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create serving certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(servingKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encode serving key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

func certificateTemplate(commonName string, now time.Time, validity time.Duration, isCA bool, dnsNames []string) (*x509.Certificate, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	usage := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	keyUsage := x509.KeyUsageDigitalSignature
	if isCA {
		usage = nil
		keyUsage |= x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	}
	return &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: commonName},
		NotBefore: now.Add(-5 * time.Minute), NotAfter: now.Add(validity),
		KeyUsage: keyUsage, ExtKeyUsage: usage, BasicConstraintsValid: true,
		IsCA: isCA, DNSNames: dnsNames,
	}, nil
}

func reconcileCABundle(ctx context.Context, client dynamic.Interface, name string, ca []byte) error {
	if len(ca) == 0 {
		return fmt.Errorf("serving Secret CA is empty")
	}
	return updateCABundle(ctx, client, name, ca)
}

func updateCABundle(ctx context.Context, client dynamic.Interface, name string, ca []byte) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		apiService, err := client.Resource(apiServiceResource).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if err := unstructured.SetNestedField(apiService.Object, base64.StdEncoding.EncodeToString(ca), "spec", "caBundle"); err != nil {
			return err
		}
		_, err = client.Resource(apiServiceResource).Update(ctx, apiService, metav1.UpdateOptions{})
		return err
	})
}

func waitForServingCertificate(ctx context.Context, opts Options, ca []byte) error {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return fmt.Errorf("new serving CA is invalid")
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS12, RootCAs: pool, ServerName: opts.ServiceName + "." + opts.Namespace + ".svc",
	}}, Timeout: 5 * time.Second}
	endpoint := "https://" + opts.ServiceName + "." + opts.Namespace + ".svc/readyz"
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err == nil {
			response, requestErr := client.Do(request)
			if requestErr == nil {
				response.Body.Close()
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for rotated serving certificate: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
