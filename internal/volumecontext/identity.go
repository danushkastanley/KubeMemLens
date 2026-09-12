package volumecontext

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

// These references describe an authorised binding, not an authorisation token
// or proof that a filesystem has not been remounted/reformatted. Redacted exports
// omit them; capture-local aliases cannot establish continuity across captures.
func evidenceKeys(scope PodScope, binding Binding) (string, string) {
	claim := binding.Configuration.Kind == PersistentClaim || binding.Configuration.Kind == EphemeralClaim
	if claim && (binding.ClaimAvailability != volumehealth.Reported || binding.PVCUID == "" || binding.PVUID == "") {
		return "", ""
	}
	reference := identityDigest("pod-volume", scope.Namespace, scope.PodUID, scope.NodeUID, binding.VolumeName, binding.PVCUID, binding.PVUID)
	if claim {
		return reference, identityDigest("claim-filesystem", scope.Namespace, binding.PVCUID, binding.PVUID)
	}
	return reference, ""
}

func identityDigest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func validEvidenceID(value string) bool {
	if value == "" {
		return true
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}
