# Build from dist/ after make build-awg-arm64.
FROM scratch
COPY --chmod=0555 keenproxy-awg-linux-arm64 /keenproxy-awg
COPY amneziawg-go-LICENSE.txt gvisor-LICENSE.txt /licenses/
USER 65534:65534
ENTRYPOINT ["/keenproxy-awg"]
CMD ["-config", "/config/awg.conf"]
