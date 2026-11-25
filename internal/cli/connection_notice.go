package cli

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/client"
)

// effectiveCollectorURL is the collector address a command will use. The
// --collector-url flag is an explicit choice of the unauthenticated path.
// MEMLENS_COLLECTOR_URL on its own is not: it used to switch every command to
// plain HTTP silently, so it now applies only with --connect-mode=http.
func (f *collectorFlags) effectiveCollectorURL() string {
	if f.collectorURL == "" && client.ConnectionMode(f.connectMode) == client.ConnectionModeHTTP {
		return f.environmentCollectorURL
	}
	return f.collectorURL
}

// connectionNotice is written to standard error before a command runs. It is
// empty for the supported, authenticated path.
func (f *collectorFlags) connectionNotice() string {
	url := f.effectiveCollectorURL()
	mode, err := client.ResolveMode(client.Options{Mode: client.ConnectionMode(f.connectMode), CollectorURL: url})
	if err != nil {
		// The command reports an invalid mode itself.
		return ""
	}
	if mode == client.ConnectionModeHTTP {
		return fmt.Sprintf("Warning: reading from the collector at %s without Kubernetes authentication or authorisation.", url)
	}
	if f.environmentCollectorURL != "" {
		return "Note: MEMLENS_COLLECTOR_URL is set but not used. Add --connect-mode=http to read from that collector over unauthenticated HTTP, or unset the variable."
	}
	return ""
}
