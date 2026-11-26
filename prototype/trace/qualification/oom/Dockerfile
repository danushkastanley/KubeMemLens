FROM docker.io/library/busybox@sha256:a46257e06c82ad85242d0d38077d0a52d9896389ca60a1a59d3463db586e5fa4
COPY workload /usr/local/bin/kml-oom-load
USER 65532:65532
CMD ["sleep", "3600"]
