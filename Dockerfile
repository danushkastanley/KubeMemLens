# syntax=docker/dockerfile:1@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89

FROM golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN LDFLAGS="-s -w -X github.com/danushkastanley/kube-memlens/internal/buildinfo.Version=${VERSION} -X github.com/danushkastanley/kube-memlens/internal/buildinfo.Commit=${COMMIT} -X github.com/danushkastanley/kube-memlens/internal/buildinfo.BuildDate=${BUILD_DATE}" && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="${LDFLAGS}" -o /out/kubectl-memlens ./cmd/kubectl-memlens && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="${LDFLAGS}" -o /out/memlens-agent ./cmd/memlens-agent && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="${LDFLAGS}" -o /out/memlens-collector ./cmd/memlens-collector

FROM scratch

COPY --from=build /out/kubectl-memlens /kubectl-memlens
COPY --from=build /out/memlens-agent /memlens-agent
COPY --from=build /out/memlens-collector /memlens-collector

USER 65532:65532

ENTRYPOINT ["/kubectl-memlens"]
