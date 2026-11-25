package sdk

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/worker/outputbuffer"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workeripc"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workerprocess"
)

const expiryDeliveryFixture = "KML_EXPIRY_DELIVERY_FIXTURE"

type deliveredEvents struct{ cache uint64 }

func (*deliveredEvents) FileActivity(trace.FileActivity) error { return workeripc.ErrProtocol }
func (d *deliveredEvents) CacheActivity(trace.CacheActivity) error {
	d.cache++
	return nil
}
func (*deliveredEvents) OOMDecision(trace.OOMDecision) error { return workeripc.ErrProtocol }

// A target with steady activity produces records until the observation ends.
// The real supervisor reads what the worker's forwarding step, protocol writer
// and output buffer send. No BPF object is loaded: the ring reader is replaced
// by a loop that offers one record every 200 microseconds until the deadline.
func TestRecordsReadUntilExpiryKeepFinalCounts(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestExpiryDeliveryWorker$")
	// Without this the race runtime sleeps for a second in os.Exit, which is
	// the whole of the supervisor's normal exit grace.
	cmd.Env = []string{expiryDeliveryFixture + "=1", "GORACE=atexit_sleep_ms=0"}
	target, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	cmd.ExtraFiles = []*os.File{target}

	now := time.Now().UTC()
	bounds := trace.DefaultBounds()
	bounds.Duration = 300 * time.Millisecond
	spec, err := trace.NewSpecification(trace.Cache, sampleIdentity(), trace.OmitPaths, bounds)
	if err != nil {
		t.Fatal(err)
	}
	request := workeripc.Request{Specification: spec, IssuedAt: now, Deadline: now.Add(bounds.Duration), ManifestSHA256: strings.Repeat("b", 64)}

	delivered := &deliveredEvents{}
	result, err := workerprocess.Run(context.Background(), cmd, request, delivered)
	if err != nil || result.Termination != trace.Expired || result.Counts.Produced == nil || result.Counts.Rejected == nil {
		t.Fatalf("records offered until expiry discarded the final counts: err=%v termination=%q delivered=%d", err, result.Termination, delivered.cache)
	}
	produced, rejected := *result.Counts.Produced, *result.Counts.Rejected
	if delivered.cache == 0 || delivered.cache+rejected != produced {
		t.Fatalf("final counts do not account for every record: produced=%d rejected=%d delivered=%d", produced, rejected, delivered.cache)
	}
}

func TestExpiryDeliveryWorker(t *testing.T) {
	if os.Getenv(expiryDeliveryFixture) != "1" {
		return
	}
	request, err := workeripc.ReadRequest(os.Stdin)
	if err != nil {
		os.Exit(31)
	}
	buffer := outputbuffer.New(os.Stdout)
	writer, err := workeripc.NewWriter(buffer, request)
	if err != nil || writer.Ready() != nil || buffer.Flush() != nil {
		os.Exit(32)
	}
	clock, err := alignDeadline(request.Deadline)
	if err != nil {
		os.Exit(33)
	}
	decoder, err := filecache.NewDecoder(request.Specification, clock)
	if err != nil {
		os.Exit(34)
	}
	var record [24]byte
	binary.LittleEndian.PutUint64(record[:8], clock.MonotonicNS)
	binary.LittleEndian.PutUint64(record[8:16], 1)
	binary.LittleEndian.PutUint32(record[16:20], 1)

	var reads readCounts
	forwardUntil := request.Deadline.Add(-deliveryMargin)
	for time.Now().Before(request.Deadline) {
		reads.seen++
		if forwardRecord(record[:], trace.Cache, decoder, writer, &reads, forwardUntil) != nil {
			os.Exit(35)
		}
		time.Sleep(200 * time.Microsecond)
	}
	counts, err := reconcileCounts(counters(reads.seen, 0, 0, 0), reads)
	if err != nil {
		os.Exit(36)
	}
	result := trace.Result{Version: trace.ContractVersion, StartedAt: request.IssuedAt, EndedAt: request.Deadline, Termination: trace.Expired, Counts: counts, Incomplete: true}
	if writer.Finish(result) != nil || buffer.Flush() != nil {
		os.Exit(37)
	}
	os.Exit(0)
}
