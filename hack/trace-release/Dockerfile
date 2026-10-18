# Public, inspected inputs only. The standard product image remains separate.
FROM scratch
ARG TARGETARCH
COPY engine/linux-${TARGETARCH}/memlens-trace /memlens-trace
COPY engine/linux-${TARGETARCH}/memlens-filecache-worker /opt/memlens-trace/memlens-filecache-worker
COPY engine/reference /opt/memlens-trace/reference
COPY programmes /opt/memlens-trace/programmes
COPY engine/verification /opt/memlens-trace/verification
COPY engine/licences /licences
COPY engine/sources /sources
USER 65532:65532
ENTRYPOINT ["/memlens-trace"]
CMD ["doctor", "--json"]
