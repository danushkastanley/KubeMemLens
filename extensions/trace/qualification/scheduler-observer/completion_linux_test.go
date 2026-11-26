package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestCompletionRequiresEOFAndPreservesInputOwnership(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := awaitCompletion(ctx, reader); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("open pipe acknowledged completion: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := awaitCompletion(context.Background(), reader); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Stat(); err != nil {
		t.Fatal("borrowed input was closed")
	}
}

func TestCompletionRejectsDataAndInvalidDescriptors(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if _, err := writer.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := awaitCompletion(context.Background(), reader); !errors.Is(err, errCompletion) {
		t.Fatal("unexpected data acknowledged completion")
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	for _, input := range []*os.File{nil, reader} {
		if err := awaitCompletion(context.Background(), input); !errors.Is(err, errCompletion) {
			t.Fatal("invalid input acknowledged completion")
		}
	}
}
