package certbootstrap

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Prepare makes sure the serving Secret exists and stops there. It runs before
// the collector is installed, when there is no APIService to reconcile and no
// server to wait for.
func Prepare(ctx context.Context, config *rest.Config, opts Options) error {
	opts, err := checked(config, opts)
	if err != nil {
		return err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	_, err = ensureSecret(ctx, client, opts)
	return err
}

func checked(config *rest.Config, opts Options) (Options, error) {
	if config == nil {
		return opts, fmt.Errorf("Kubernetes config is required")
	}
	if opts.Namespace == "" || opts.SecretName == "" || opts.APIService == "" || opts.ServiceName == "" {
		return opts, fmt.Errorf("namespace, Secret, APIService and Service names are required")
	}
	if opts.Validity <= 0 || opts.RotateBefore <= 0 || opts.RotateBefore >= opts.Validity {
		return opts, fmt.Errorf("certificate validity and rotation window are invalid")
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	return opts, nil
}

// ensureSecret returns the serving Secret, creating it with new material when
// it is absent. An existing Secret is returned as it is; rotation is Run's job.
func ensureSecret(ctx context.Context, client kubernetes.Interface, opts Options) (*corev1.Secret, error) {
	secrets := client.CoreV1().Secrets(opts.Namespace)
	secret, err := secrets.Get(ctx, opts.SecretName, metav1.GetOptions{})
	if err == nil {
		return secret, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("get serving Secret: %w", err)
	}
	caPEM, certPEM, keyPEM, err := generate(opts, opts.Now())
	if err != nil {
		return nil, err
	}
	created, err := secrets.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: opts.Namespace, Name: opts.SecretName},
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{"ca.crt": caPEM, corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM},
	}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		// Another bootstrap created it first; its material is the one in use.
		secret, err = secrets.Get(ctx, opts.SecretName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get serving Secret: %w", err)
		}
		return secret, nil
	}
	if err != nil {
		return nil, fmt.Errorf("create serving Secret: %w", err)
	}
	return created, nil
}
