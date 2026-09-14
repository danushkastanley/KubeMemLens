FROM golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build
WORKDIR /src
COPY go.mod go.sum LICENSE NOTICE ./
COPY internal/tracepreflight ./internal/tracepreflight
COPY internal/trace ./internal/trace
COPY internal/traceframe ./internal/traceframe
COPY internal/tracesession ./internal/tracesession
COPY internal/traceadmission ./internal/traceadmission
COPY internal/kubeauth ./internal/kubeauth
COPY prototype/trace ./prototype/trace
WORKDIR /src/prototype/trace
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -buildvcs=false -o /memlens-trace ./cmd/memlens-trace
RUN CGO_ENABLED=0 go run ./licencebundle --output /licences

FROM scratch
COPY --from=build /memlens-trace /memlens-trace
COPY --from=build /licences /licences
COPY --from=build /usr/local/go/LICENSE /licences/Go.txt
COPY prototype/trace/testdata/reference /opt/memlens-trace/reference
USER 65532:65532
ENTRYPOINT ["/memlens-trace"]
CMD ["doctor", "--json"]
