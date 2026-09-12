FROM golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build
WORKDIR /src
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -o /fixture main.go certs.go
FROM scratch
COPY --from=build /fixture /fixture
USER 65532:65532
ENTRYPOINT ["/fixture"]
