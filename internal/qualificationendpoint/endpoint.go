// Package qualificationendpoint restricts endpoints used by qualification tools.
// Provider identity and CA pins must also be verified by the parent controller.
package qualificationendpoint

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"regexp"
)

type Scope string

const (
	Local Scope = "local"
	EKS   Scope = "eks"
)

func (s *Scope) UnmarshalJSON(data []byte) error {
	var value string
	if json.Unmarshal(data, &value) != nil || (value != string(Local) && value != string(EKS)) {
		return errors.New("invalid qualification network scope")
	}
	*s = Scope(value)
	return nil
}

var kindHost = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,100}-control-plane$`)

// Bound DNS labels and the regional suffix, including the optional EKS routing
// label. These forms cover the approved commercial AWS partition only.
var eksHost = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)?\.[a-z]{2}-[a-z]{1,20}-[1-9]\.(?:eks\.amazonaws\.com|api\.aws)$`)

func Allowed(server string, scope Scope) bool {
	endpoint, err := url.Parse(server)
	if err != nil || len(server) > 300 || endpoint.Scheme != "https" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" || endpoint.String() != server {
		return false
	}
	switch scope {
	case "", Local:
		if endpoint.Port() == "" {
			return false
		}
		host := endpoint.Hostname()
		ip := net.ParseIP(host)
		return (ip != nil && ip.IsLoopback()) || kindHost.MatchString(host) || (host == "kubernetes.default.svc" && endpoint.Port() == "443")
	case EKS:
		return (endpoint.Port() == "" || endpoint.Port() == "443") && eksHost.MatchString(endpoint.Hostname()) && endpoint.Host != endpoint.Hostname()+":"
	default:
		return false
	}
}
