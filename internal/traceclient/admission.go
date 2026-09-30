package traceclient

import (
	"context"
	"errors"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

type admissionDocument struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	State        string    `json:"state"`
	ExpiresAt    time.Time `json:"expiresAt"`
	EngineDigest string    `json:"engineDigest"`
}

func validID(id string) bool { return len(id) == 32 && strings.Trim(id, "0123456789abcdef") == "" }
func validDigest(digest string) bool {
	return len(digest) == 71 && strings.HasPrefix(digest, "sha256:") && strings.Trim(digest[7:], "0123456789abcdef") == ""
}
func admissionPath(namespace, id string) (string, error) {
	if len(validation.IsDNS1123Label(namespace)) != 0 || !validID(id) {
		return "", failure(Invalid)
	}
	return apiPrefix + "/namespaces/" + namespace + "/traces/" + id, nil
}

func (c *Client) parseAdmission(data []byte, namespace, id string) (Admission, error) {
	var d admissionDocument
	if decode(data, &d) != nil || d.APIVersion != "tracing.kubememlens.io/v1alpha1" || d.Kind != "TraceAdmission" || d.Metadata.Namespace != namespace || !validID(d.Metadata.Name) || (id != "" && d.Metadata.Name != id) || (d.State != "admitted" && d.State != "active") || d.ExpiresAt.IsZero() || !validDigest(d.EngineDigest) {
		return Admission{}, failure(Protocol)
	}
	return Admission{client: c, namespace: namespace, id: d.Metadata.Name, state: d.State, engine: d.EngineDigest, expires: d.ExpiresAt}, nil
}

// Create never retries. A nonzero Admission returned with an error identifies
// an owned response that the session coordinator must explicitly cancel.
func (c *Client) Create(ctx context.Context, plan Plan) (Admission, error) {
	if plan.client != c || c == nil {
		return Admission{}, failure(Invalid)
	}
	if err := ctx.Err(); err != nil {
		return Admission{}, err
	}
	body, err := requestData(plan.selection, plan.intent)
	if err != nil {
		return Admission{}, err
	}
	data, status, err := c.control(ctx, "POST", apiPrefix+"/namespaces/"+plan.selection.Namespace+"/traces", body, 201, 4096)
	if err != nil {
		var local *Error
		if status == 0 && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &local) && local.Kind == Capacity)) {
			return Admission{}, err
		}
		if status == 0 || status == 201 || status >= 500 {
			return Admission{}, failure(Uncertain)
		}
		return Admission{}, err
	}
	a, err := c.parseAdmission(data, plan.selection.Namespace, "")
	if err != nil {
		return Admission{}, failure(Uncertain)
	}
	a.plan = &plan
	if a.state != "admitted" || a.engine != plan.document.Node.EngineDigest {
		return a, failure(TargetChanged)
	}
	return a, nil
}

// Inspect recovers an existing owned admission, without selecting a new target.
// Its first stream metadata supplies the immutable server-held intent; a plan
// created in this process additionally checks that metadata against preflight.
func (c *Client) Inspect(ctx context.Context, namespace, id string) (Admission, error) {
	path, err := admissionPath(namespace, id)
	if err != nil {
		return Admission{}, err
	}
	data, _, err := c.control(ctx, "GET", path, nil, 200, 4096)
	if err != nil {
		return Admission{}, err
	}
	return c.parseAdmission(data, namespace, id)
}

func (c *Client) Cancel(ctx context.Context, admission Admission) (Cleanup, error) {
	if admission.client != c || c == nil {
		return CleanupUnconfirmed, failure(Invalid)
	}
	return c.CancelID(ctx, admission.namespace, admission.id)
}

// CancelID is an explicit operator action against an exact namespace and ID.
// Absence, expiry, denial and transport failure do not establish cleanup.
func (c *Client) CancelID(ctx context.Context, namespace, id string) (Cleanup, error) {
	path, err := admissionPath(namespace, id)
	if err != nil {
		return CleanupUnconfirmed, err
	}
	data, _, err := c.control(ctx, "DELETE", path, nil, 200, 4096)
	if err != nil {
		return CleanupUnconfirmed, err
	}
	var status metav1.Status
	if decode(data, &status) != nil || status.Kind != "Status" || status.APIVersion != "v1" || status.Status != metav1.StatusSuccess || status.Code != 200 {
		return CleanupUnconfirmed, failure(Uncertain)
	}
	return CleanupConfirmed, nil
}
