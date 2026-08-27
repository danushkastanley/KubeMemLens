.PHONY: test coverage test-race build run-sample-top run-sample-explain fmt fmt-check check-support-contract check-scale-contract check-provider-contract check-terminal-contract check-release-contract check-community-contract check-community-settings vet vuln check e2e-kind verify-auth-architecture-kind verify-authenticated-ingestion-kind verify-tenant-scoped-reads-kind verify-tenant-isolation-kind verify-scale-capacity qualify-cluster soak-live-density

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

check-provider-contract:
	python3 -m unittest discover -s hack/provider-profiles -p 'test_*.py'
	python3 -m unittest discover -s hack/provider-inventory -p 'test_*.py'
	python3 hack/test_verify_chart_archive.py
	hack/test-provider-evidence.sh
	hack/test-provider-cleanup.sh
	hack/verify-provider-chart.sh

check-terminal-contract:
	python3 -m unittest discover -s hack/terminal-qualification -p 'test_*.py'
	@for bundle in docs/qualification-results/terminal-runtime-*; do \
		[ -d "$$bundle" ] || continue; \
		python3 hack/terminal-qualification/validate_evidence.py "$$bundle"; \
	done
	@output=$$(mktemp); \
		if hack/qualify-linux-terminals.sh >"$$output" 2>&1; then \
			echo "Linux terminal qualification ran without explicit acknowledgement"; \
			exit 1; \
		fi; \
		grep -q 'TERMINAL_LINUX_ACKNOWLEDGE' "$$output"; \
		rm -f "$$output"

check-release-contract:
	python3 -m unittest discover -s hack/release -p 'test_*.py'
	hack/release/test_create_draft.sh
	hack/release/test_publish_candidate_draft.sh
	hack/release/test_publish_candidate_resume.sh
	hack/release/test_resume_draft.sh
	hack/release/test_validate_candidate_manifest.sh
	hack/release/test_validate_tag.sh
	hack/check-release-contract.sh

check-community-contract:
	hack/check-community-contract.sh

check-community-settings:
	hack/community/check_repository_settings.sh

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

check: fmt-check check-support-contract check-scale-contract check-provider-contract check-terminal-contract check-release-contract check-community-contract test coverage test-race vet vuln build

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
