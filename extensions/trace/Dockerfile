FROM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
WORKDIR /src
COPY go.mod go.sum LICENSE NOTICE ./
COPY internal/tracepreflight ./internal/tracepreflight
COPY internal/trace ./internal/trace
COPY internal/traceaudit ./internal/traceaudit
COPY internal/tracecompat ./internal/tracecompat
COPY internal/traceaggregate ./internal/traceaggregate
COPY internal/traceevidence ./internal/traceevidence
COPY internal/cgroup ./internal/cgroup
COPY internal/model ./internal/model
COPY internal/traceframe ./internal/traceframe
COPY internal/tracesession ./internal/tracesession
COPY internal/traceadmission ./internal/traceadmission
COPY internal/kubeauth ./internal/kubeauth
COPY extensions/trace ./extensions/trace
WORKDIR /src/extensions/trace
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -buildvcs=false -o /memlens-trace ./cmd/memlens-trace
RUN CGO_ENABLED=0 go run ./licencebundle --output /licences

FROM scratch
COPY --from=build /memlens-trace /memlens-trace
COPY --from=build /licences /licences
COPY --from=build /usr/local/go/LICENSE /licences/Go.txt
COPY extensions/trace/testdata/reference /opt/memlens-trace/reference
USER 65532:65532
ENTRYPOINT ["/memlens-trace"]
CMD ["doctor", "--json"]
