package traceaudit

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type blockedWriter struct {
	entered, release chan struct{}
	once             sync.Once
}

func (w *blockedWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}

type brokenWriter struct{ short bool }

func (w brokenWriter) Write(p []byte) (int, error) {
	if w.short {
		return len(p) - 1, nil
	}
	return 0, errors.New("private sink credential")
}
func auditRecord(t *testing.T) Record {
	t.Helper()
	r, err := NewRecord(eventFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWriterReportsFullWritesAndSanitisesFailures(t *testing.T) {
	record := auditRecord(t)
	var output bytes.Buffer
	writer, err := NewWriter(&output)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Write(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	expected, _ := record.Bytes()
	if !bytes.Equal(output.Bytes(), expected) {
		t.Fatal("audit receipt preceded full write")
	}
	for _, short := range []bool{false, true} {
		writer, _ := NewWriter(brokenWriter{short: short})
		if err := writer.Write(context.Background(), record); err != ErrUnavailable {
			t.Fatal("write failure hidden or disclosed")
		}
		if writer.Healthy() != ErrUnavailable || writer.Write(context.Background(), record) != ErrUnavailable {
			t.Fatal("failed sink accepted another record")
		}
		if err := writer.Close(context.Background()); err != ErrUnavailable {
			t.Fatal("failed delivery forgotten at shutdown")
		}
	}
}
func TestBlockedAuditHonoursCallerAndShutdownDeadlines(t *testing.T) {
	sink := &blockedWriter{entered: make(chan struct{}), release: make(chan struct{})}
	writer, _ := NewWriter(sink)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	record := auditRecord(t)
	go func() { result <- writer.Write(ctx, record) }()
	select {
	case <-sink.entered:
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}
	cancel()
	select {
	case err := <-result:
		if err != ErrUnavailable {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked sink retained caller")
	}
	if writer.Write(context.Background(), record) != ErrUnavailable {
		t.Fatal("uncertain sink accepted new work")
	}
	stopped, stop := context.WithCancel(context.Background())
	stop()
	if writer.Close(stopped) != ErrUnavailable {
		t.Fatal("shutdown ignored blocked writer")
	}
	close(sink.release)
	if writer.Close(context.Background()) != ErrUnavailable {
		t.Fatal("uncertain receipt became success")
	}
}

func TestAuditQueueSaturationCannotCreateExtraWorkersOrUnboundedRecords(t *testing.T) {
	sink := &blockedWriter{entered: make(chan struct{}), release: make(chan struct{})}
	writer, _ := NewWriter(sink)
	record := auditRecord(t)
	first := make(chan error, 1)
	go func() { first <- writer.Write(context.Background(), record) }()
	select {
	case <-sink.entered:
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}
	results := make(chan error, QueueCapacity+1)
	for range QueueCapacity + 1 {
		go func() { results <- writer.Write(context.Background(), record) }()
	}
	select {
	case err := <-results:
		if err != ErrUnavailable {
			t.Fatal("queue saturation hidden")
		}
	case <-time.After(time.Second):
		t.Fatal("audit queue did not enforce capacity")
	}
	if len(writer.queue) != QueueCapacity {
		t.Fatal("unexpected pending-record bound")
	}
	stopped, stop := context.WithCancel(context.Background())
	stop()
	if writer.Close(stopped) != ErrUnavailable {
		t.Fatal("blocked sink appeared drained")
	}
	close(sink.release)
	if writer.Close(context.Background()) != ErrUnavailable {
		t.Fatal("saturation was forgotten")
	}
	if <-first != ErrUnavailable {
		t.Fatal("blocked first write became confirmed after failure")
	}
	for range QueueCapacity {
		if <-results != ErrUnavailable {
			t.Fatal("queued write became confirmed after saturation")
		}
	}
	if len(writer.queue) != 0 {
		t.Fatal("closed writer retained pending audit records")
	}
}
