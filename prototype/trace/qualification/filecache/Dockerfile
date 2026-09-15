# Context: independently compiled static workload and an empty work directory.
FROM docker.io/library/busybox@sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028
COPY workload /usr/local/bin/kml-io-workload
COPY --chown=65532:65532 work /work
USER 65532:65532
CMD ["sleep", "3600"]
