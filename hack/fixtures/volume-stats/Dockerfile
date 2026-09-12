FROM golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags="-X main.version=eccd681b18a2c96332f33c2cac5db38656edd500" -o /out/hostpathplugin ./cmd/hostpathplugin
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -o /out/volume-seed ./cmd/volume-seed
FROM kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5
COPY --from=build /out/hostpathplugin /hostpathplugin
COPY --from=build /out/volume-seed /volume-seed
USER 65532:65532
ENTRYPOINT ["/hostpathplugin"]
