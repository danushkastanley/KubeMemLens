FROM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags="-X main.version=eccd681b18a2c96332f33c2cac5db38656edd500" -o /out/hostpathplugin ./cmd/hostpathplugin
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -o /out/healthctl ./cmd/healthctl
FROM kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5
COPY --from=build /out/hostpathplugin /hostpathplugin
COPY --from=build /out/healthctl /healthctl
USER 65532:65532
ENTRYPOINT ["/hostpathplugin"]
