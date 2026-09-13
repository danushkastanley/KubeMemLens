FROM golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build
WORKDIR /src
COPY go.mod go.sum LICENSE ./
COPY internal/tracepreflight ./internal/tracepreflight
COPY prototype/trace ./prototype/trace
WORKDIR /src/prototype/trace
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -buildvcs=false -o /memlens-trace ./cmd/memlens-trace

FROM scratch
COPY --from=build /memlens-trace /memlens-trace
COPY --from=build /src/LICENSE /licences/KubeMemLens.txt
COPY --from=build /usr/local/go/LICENSE /licences/Go.txt
COPY --from=build /go/pkg/mod/github.com/cilium/ebpf@v0.22.0/LICENSE /licences/cilium-ebpf.txt
COPY --from=build /go/pkg/mod/golang.org/x/sys@v0.47.0/LICENSE /licences/x-sys.txt
COPY prototype/trace/testdata/reference /opt/memlens-trace/reference
USER 0:0
ENTRYPOINT ["/memlens-trace"]
CMD ["doctor", "--json"]
