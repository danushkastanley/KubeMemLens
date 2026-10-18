package sdk

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
)

type readCounts struct{ seen, forwarded uint64 }

func readEvents(ctx context.Context, owned *resources, decoder *filecache.Decoder, output trace.Output, cancel context.CancelFunc) (counts readCounts, err error) {
	finishInterrupt := interruptRead(ctx, owned.reader)
	defer func() {
		if finishInterrupt() != nil {
			err = ErrWorker
		}
	}()
	var record ringbuf.Record
	nextValidation := time.Now()
	for {
		if ctx.Err() != nil {
			return counts, nil
		}
		now := time.Now()
		if !now.Before(nextValidation) {
			if targetfs.VerifyWorkerDescriptor(ctx, owned.target, owned.spec.Target()) != nil {
				cancel()
				return counts, ErrWorker
			}
			nextValidation = now.Add(time.Second)
		}
		// Approved programmes notify the ring on submission. Only target
		// revalidation needs a timed wake-up; cancellation flushes the reader.
		owned.reader.SetDeadline(nextValidation)
		if err := owned.reader.ReadInto(&record); err != nil {
			if interruptedRead(ctx, err) {
				return counts, nil
			}
			if errors.Is(err, os.ErrDeadlineExceeded) {
				// epoll rounds deadlines to milliseconds. Revalidate immediately
				// instead of spinning on an already elapsed/rounded deadline.
				nextValidation = time.Time{}
				continue
			}
			cancel()
			return counts, ErrWorker
		}
		counts.seen++
		if counts.seen > owned.spec.Bounds().Events {
			cancel()
			return counts, ErrWorker
		}
		if ctx.Err() != nil {
			return counts, nil
		}
		if err := forwardRecord(record.RawSample, owned.spec.Kind(), decoder, output, &counts); err != nil {
			cancel()
			return counts, err
		}
	}
}

func forwardRecord(data []byte, kind trace.Kind, decoder *filecache.Decoder, output trace.Output, counts *readCounts) error {
	if kind == trace.OOM {
		event, err := decoder.OOM(data)
		if err != nil {
			return ErrWorker
		}
		counts.forwarded++
		return output.OOMDecision(event)
	}
	if kind == trace.Files {
		event, err := decoder.File(data)
		if err != nil {
			return ErrWorker
		}
		counts.forwarded++
		return output.FileActivity(event)
	}
	event, err := decoder.Cache(data)
	if err != nil {
		return ErrWorker
	}
	counts.forwarded++
	return output.CacheActivity(event)
}

func finalCounts(owned *resources, reads readCounts) (trace.Counts, error) {
	data, err := owned.maps["counts"].LookupBytes(uint32(0))
	if err != nil {
		return trace.Counts{}, ErrWorker
	}
	counts, err := filecache.DecodeCounts(data)
	if err != nil {
		return trace.Counts{}, ErrWorker
	}
	return reconcileCounts(counts, reads)
}

// Counts are sampled after detachment. Emitted records never passed to Output
// (including unread ring records) are rejected by the worker. Callback outcomes
// belong to the session, so they are not counted again here.
func reconcileCounts(counts trace.Counts, reads readCounts) (trace.Counts, error) {
	if counts.Produced == nil || counts.Sampled == nil || counts.Lost == nil || counts.Rejected == nil || reads.forwarded > reads.seen {
		return trace.Counts{}, ErrWorker
	}
	remaining := *counts.Produced
	for _, value := range []uint64{*counts.Sampled, *counts.Lost, *counts.Rejected} {
		if value > remaining {
			return trace.Counts{}, ErrWorker
		}
		remaining -= value
	}
	if reads.seen > remaining {
		return trace.Counts{}, ErrWorker
	}
	// This addition cannot overflow: both categories partition produced above.
	rejected := *counts.Rejected + remaining - reads.forwarded
	counts.Rejected = &rejected
	return counts, nil
}

// Cancellation explains only the expected flush/deadline interruption. Other
// reader failures must still invalidate the engine's terminal evidence.
func interruptedRead(ctx context.Context, err error) bool {
	return ctx.Err() != nil && (errors.Is(err, ringbuf.ErrFlushed) || errors.Is(err, os.ErrDeadlineExceeded))
}
