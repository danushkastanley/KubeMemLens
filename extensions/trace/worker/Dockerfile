# Build context contains verified candidate artefacts only. No installation
# policy or private signing key belongs in this image.
FROM scratch
COPY memlens-trace /memlens-trace
COPY memlens-filecache-worker /opt/memlens-trace/memlens-filecache-worker
COPY reference /opt/memlens-trace/reference
COPY programmes /opt/memlens-trace/programmes
COPY verification /opt/memlens-trace/verification
COPY licences /licences
COPY sources /sources
USER 65532:65532
ENTRYPOINT ["/memlens-trace"]
CMD ["doctor", "--json"]
