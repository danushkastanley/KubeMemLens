package traceframe

import (
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

// A node whose clock is behind shortens the deadline to its own clock. The
// controller accepts that only within the allowance, and never an extension.
func FuzzMatchAdmissionDeadlineWithinSkewAllowance(f *testing.F) {
	for _, seed := range []int64{0, 1, int64(200 * time.Millisecond), int64(ClockSkewAllowance), int64(ClockSkewAllowance) + 1, -1, int64(-time.Second), int64(10 * time.Second)} {
		f.Add(seed)
	}
	id, engine, programme := strings.Repeat("b", 32), "sha256:"+strings.Repeat("c", 64), "sha256:"+strings.Repeat("d", 64)
	frameDeadline := time.Unix(230, 0).UTC()
	f.Fuzz(func(t *testing.T, nodeShortenedBy int64) {
		if nodeShortenedBy > int64(time.Hour) || nodeShortenedBy < -int64(time.Hour) {
			t.Skip()
		}
		frame := metadata(t)
		base := spec(t, trace.Files, trace.OmitPaths)
		lease := frameDeadline.Add(time.Duration(nodeShortenedBy))
		accepted := frame.MatchAdmission(id, engine, programme, base, lease) == nil
		allowed := nodeShortenedBy >= 0 && nodeShortenedBy <= int64(ClockSkewAllowance)
		if accepted != allowed {
			t.Fatalf("frame deadline %v before the lease deadline: accepted=%v, want %v", time.Duration(nodeShortenedBy), accepted, allowed)
		}
	})
}
