.PHONY: test coverage test-race build run-sample-top run-sample-explain fmt fmt-check check-support-contract check-scale-contract vet vuln check e2e-kind verify-auth-architecture-kind verify-authenticated-ingestion-kind verify-tenant-scoped-reads-kind verify-tenant-isolation-kind verify-scale-capacity qualify-cluster soak-live-density

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

check-support-contract:
	hack/check-support-contract.sh

check-scale-contract:
	python3 -m unittest discover -s hack/scale-profiles -p 'test_*.py'
	python3 hack/test_observe_kind_telemetry.py
	hack/test-density-libraries.sh
	hack/verify-scale-capacity.sh

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

check: fmt-check check-support-contract check-scale-contract test coverage test-race vet vuln build

e2e-kind:
	hack/e2e-kind.sh

verify-auth-architecture-kind:
	hack/verify-auth-architecture-kind.sh

verify-authenticated-ingestion-kind:
	hack/verify-authenticated-ingestion-kind.sh

verify-tenant-scoped-reads-kind:
	hack/verify-tenant-scoped-reads-kind.sh

verify-tenant-isolation-kind:
	hack/verify-tenant-isolation-kind.sh

verify-scale-capacity:
	hack/verify-scale-capacity.sh

qualify-cluster:
	hack/qualify-cluster.sh

soak-live-density:
	hack/soak-live-density.sh
