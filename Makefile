.PHONY: test build run-sample-top run-sample-explain fmt vet

test:
	go test ./...

build:
	go build ./cmd/kubectl-memlens
	go build ./cmd/memlens-agent
	go build ./cmd/memlens-collector

run-sample-top:
	go run ./cmd/kubectl-memlens sample top

run-sample-explain:
	go run ./cmd/kubectl-memlens sample explain cache-heavy

fmt:
	gofmt -w .

vet:
	go vet ./...
