# Context: independently compiled static workload and an empty work directory.
FROM docker.io/library/busybox@sha256:a46257e06c82ad85242d0d38077d0a52d9896389ca60a1a59d3463db586e5fa4
COPY workload /usr/local/bin/kml-io-workload
COPY --chown=65532:65532 work /work
USER 65532:65532
CMD ["/usr/local/bin/kml-io-workload", "idle"]
