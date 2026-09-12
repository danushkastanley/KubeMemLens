package nodestats

import (
	"context"
	"net/http"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	"k8s.io/apimachinery/pkg/util/validation"
)

const identityPath = "/apis/authentication.k8s.io/v1/selfsubjectreviews"
const maxIdentityBytes = 64 << 10

type nodeIdentity struct{ name, uid string }

// The API server, not a flag or locally decoded JWT, attests the Pod-bound
// token's Node identity. This also rejects an ordinary unbound admin credential.
func (s *Source) identity(ctx context.Context) (nodeIdentity, error) {
	body := strings.NewReader(`{"apiVersion":"authentication.k8s.io/v1","kind":"SelfSubjectReview"}`)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+identityPath, body)
	if err != nil {
		return nodeIdentity{}, &Error{Reason: nodecontext.Authentication}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	data, err := readResponse(s.api, request, maxIdentityBytes, http.StatusCreated)
	if err != nil {
		return nodeIdentity{}, err
	}
	return decodeIdentity(ctx, data, s.opts.NodeName)
}

func decodeIdentity(ctx context.Context, data []byte, expected string) (nodeIdentity, error) {
	if len(data) > maxIdentityBytes {
		return nodeIdentity{}, &Error{Reason: nodecontext.ResponseTooLarge}
	}
	var apiVersion, kind, username, podUID, credential string
	identity := nodeIdentity{}
	d := newDecoder(ctx, data)
	err := d.object(map[string]func() error{
		"apiVersion": func() error { return d.stringInto(&apiVersion, 64) },
		"kind":       func() error { return d.stringInto(&kind, 32) },
		"status": func() error {
			return d.object(map[string]func() error{
				"userInfo": func() error {
					return d.object(map[string]func() error{
						"username": func() error { return d.stringInto(&username, 384) },
						"extra": func() error {
							return d.object(map[string]func() error{
								"authentication.kubernetes.io/node-name":     func() error { return d.oneString(&identity.name, nodecontext.MaxNodeNameBytes) },
								"authentication.kubernetes.io/node-uid":      func() error { return d.oneString(&identity.uid, nodecontext.MaxNodeUIDBytes) },
								"authentication.kubernetes.io/pod-uid":       func() error { return d.oneString(&podUID, nodecontext.MaxNodeUIDBytes) },
								"authentication.kubernetes.io/credential-id": func() error { return d.oneString(&credential, 256) },
							})
						},
					})
				},
			})
		},
	})
	if err != nil || d.finish() != nil || apiVersion != "authentication.k8s.io/v1" || kind != "SelfSubjectReview" || !serviceAccountUsername(username) || !validUID(podUID) || credential == "" || !validUID(identity.uid) {
		return nodeIdentity{}, &Error{Reason: nodecontext.Authentication}
	}
	if identity.name != expected {
		return nodeIdentity{}, &Error{Reason: nodecontext.InvalidTarget}
	}
	return identity, nil
}

func (d *decoder) oneString(value *string, maximum int) error {
	count := 0
	err := d.array(func(index int) error {
		if index != 0 {
			return errJSON
		}
		count++
		return d.stringInto(value, maximum)
	})
	if err != nil || count != 1 || *value == "" {
		return errJSON
	}
	return nil
}

func serviceAccountUsername(value string) bool {
	parts := strings.Split(value, ":")
	return len(parts) == 4 && parts[0] == "system" && parts[1] == "serviceaccount" &&
		len(validation.IsDNS1123Label(parts[2])) == 0 && len(validation.IsDNS1123Subdomain(parts[3])) == 0
}
