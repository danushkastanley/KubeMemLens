package certbootstrap

import (
	"bytes"
	"context"
	"crypto/tls"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func ensureOptions() Options {
	return Options{
		Namespace: "kube-memlens", SecretName: "serving", APIService: "v1alpha1.memory.kubememlens.io",
		ServiceName: "kube-memlens-collector", Validity: 24 * time.Hour, RotateBefore: time.Hour,
		Now: func() time.Time { return time.Unix(1_800_000_000, 0).UTC() },
	}
}

func TestEnsureSecretCreatesUsableMaterialWhenTheSecretIsAbsent(t *testing.T) {
	client := fake.NewSimpleClientset()
	opts := ensureOptions()

	created, err := ensureSecret(context.Background(), client, opts)
	if err != nil {
		t.Fatalf("ensureSecret: %v", err)
	}
	if created.Type != corev1.SecretTypeTLS {
		t.Fatalf("Secret type = %q, want %q", created.Type, corev1.SecretTypeTLS)
	}
	if _, err := tls.X509KeyPair(created.Data[corev1.TLSCertKey], created.Data[corev1.TLSPrivateKeyKey]); err != nil {
		t.Fatalf("created serving key pair is unusable: %v", err)
	}
	if needsRotation(created, opts, opts.Now()) {
		t.Fatal("a Secret created a moment ago is reported as due for rotation")
	}
	stored, err := client.CoreV1().Secrets(opts.Namespace).Get(context.Background(), opts.SecretName, metav1.GetOptions{})
	if err != nil || !bytes.Equal(stored.Data[corev1.TLSPrivateKeyKey], created.Data[corev1.TLSPrivateKeyKey]) {
		t.Fatalf("stored Secret does not hold the created key: err=%v", err)
	}
}

func TestEnsureSecretLeavesAnExistingSecretAlone(t *testing.T) {
	opts := ensureOptions()
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: opts.Namespace, Name: opts.SecretName},
		Data:       map[string][]byte{corev1.TLSPrivateKeyKey: []byte("operator-supplied")},
	}
	client := fake.NewSimpleClientset(existing)

	got, err := ensureSecret(context.Background(), client, opts)
	if err != nil {
		t.Fatalf("ensureSecret: %v", err)
	}
	if string(got.Data[corev1.TLSPrivateKeyKey]) != "operator-supplied" {
		t.Fatalf("existing Secret was replaced: %q", got.Data[corev1.TLSPrivateKeyKey])
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatalf("ensureSecret wrote to an existing Secret: %s", action.GetVerb())
		}
	}
}

// Two bootstrap Jobs can start together when hook ordering is not honoured.
// The one that loses the create must use the winner's Secret, not its own
// unsaved material, or the caBundle would not match the serving certificate.
func TestEnsureSecretUsesTheSecretAnotherWriterCreatedFirst(t *testing.T) {
	opts := ensureOptions()
	client := fake.NewSimpleClientset()
	winner := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: opts.Namespace, Name: opts.SecretName},
		Data:       map[string][]byte{"ca.crt": []byte("winner-ca")},
	}
	client.PrependReactor("create", "secrets", func(clienttesting.Action) (bool, runtime.Object, error) {
		if err := client.Tracker().Add(winner); err != nil {
			t.Fatalf("add the winning Secret: %v", err)
		}
		return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "secrets"}, opts.SecretName)
	})

	got, err := ensureSecret(context.Background(), client, opts)
	if err != nil {
		t.Fatalf("ensureSecret: %v", err)
	}
	if string(got.Data["ca.crt"]) != "winner-ca" {
		t.Fatalf("ensureSecret returned %q, want the Secret that was created first", got.Data["ca.crt"])
	}
}
