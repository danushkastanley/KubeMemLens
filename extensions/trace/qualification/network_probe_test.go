package qualification

import (
	"net"
	"os"
	"testing"
	"time"
)

// An administrator supplies an owned Service IP, avoiding DNS as a confounder
// when proving the node policy's denied egress. This is a test binary only.
func TestLiveStreamNetworkPolicy(t *testing.T) {
	if os.Getenv("KML_NETWORK_QUALIFICATION") != "owned-local-kind-cni" {
		t.Skip("requires owned local CNI fixture")
	}
	host, port, err := net.SplitHostPort(os.Getenv("KML_NETWORK_ENDPOINT"))
	if err != nil || net.ParseIP(host) == nil || (port != "9443" && port != "8443") {
		t.Fatal("invalid fixture service endpoint")
	}
	expected := os.Getenv("KML_NETWORK_EXPECT")
	if expected != "connected" && expected != "denied" {
		t.Fatal("explicit network outcome required")
	}
	connection, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 2*time.Second)
	if connection != nil {
		connection.Close()
	}
	if expected == "connected" {
		if err != nil {
			t.Fatal("authorised fixture peer did not connect")
		}
		return
	}
	failure, ok := err.(net.Error)
	if !ok || !failure.Timeout() {
		t.Fatal("denial was not an enforced connection timeout")
	}
}
