# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/kubectl-memlens ./cmd/kubectl-memlens && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/memlens-agent ./cmd/memlens-agent && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/memlens-collector ./cmd/memlens-collector

FROM scratch

COPY --from=build /out/kubectl-memlens /kubectl-memlens
COPY --from=build /out/memlens-agent /memlens-agent
COPY --from=build /out/memlens-collector /memlens-collector

USER 65532:65532

ENTRYPOINT ["/kubectl-memlens"]
