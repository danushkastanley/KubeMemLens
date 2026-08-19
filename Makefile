.PHONY: test coverage test-race build run-sample-top run-sample-explain fmt fmt-check vet vuln check e2e-kind qualify-cluster soak-live-density

VERSION ?= dev
COMMIT ?= unknown
BUILD_DATE ?= unknown
LDFLAGS = -s -w \
	-X github.com/danushkastanley/kube-memlens/internal/buildinfo.Version=$(VERSION) \
	-X github.com/danushkastanley/kube-memlens/internal/buildinfo.Commit=$(COMMIT) \
	-X github.com/danushkastanley/kube-memlens/internal/buildinfo.BuildDate=$(BUILD_DATE)

test:
	go test ./...

coverage:
	go test -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

test-race:
	go test -race ./...

build:
	go build -trimpath -ldflags "$(LDFLAGS)" ./cmd/...

run-sample-top:
	go run ./cmd/kubectl-memlens sample top

run-sample-explain:
	go run ./cmd/kubectl-memlens sample explain cache-heavy

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

vet:
	go vet ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

check: fmt-check test coverage test-race vet vuln build

e2e-kind:
	hack/e2e-kind.sh

qualify-cluster:
	hack/qualify-cluster.sh

soak-live-density:
	hack/soak-live-density.sh
