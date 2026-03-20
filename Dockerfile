FROM scratch
COPY --chmod=0755 bin/manager /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
