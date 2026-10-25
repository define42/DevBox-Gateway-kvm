# syntax=docker/dockerfile:1

# Production binaries use only Go and do not need a C compiler or native SDK.
FROM golang:1.26-trixie AS build
RUN test "$(dpkg --print-architecture)" = amd64

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY *.go ./
COPY internal/ ./internal/
COPY kvm/ ./kvm/
COPY pkg/ ./pkg/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false \
        -ldflags="-s -w" -o /out/devbox-kvm .

FROM debian:trixie-slim AS runtime-deps

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 devbox \
    && useradd --uid 10001 --gid 10001 --no-create-home \
        --home-dir /var/lib/devbox-kvm --shell /usr/sbin/nologin devbox \
    && install -d -m 0700 -o devbox -g devbox /var/lib/devbox-kvm

COPY --from=build /out/devbox-kvm /usr/local/bin/devbox-kvm
COPY LICENSE LICENSE-OpenH264 LICENSE-go.264 /usr/share/doc/devbox-gateway-kvm/
COPY internal/qcow2import/LICENSE /usr/share/doc/devbox-gateway-kvm/LICENSE-go-qcow2reader
COPY internal/gateway/firmware/LICENSE /usr/share/doc/devbox-gateway-kvm/LICENSE-OVMF

ENV DATA_ROOT_DIR=/var/lib/devbox-kvm \
    LISTEN_ADDR=0.0.0.0:8443

USER 10001:10001
WORKDIR /var/lib/devbox-kvm
EXPOSE 8443/tcp
ENTRYPOINT ["/usr/local/bin/devbox-kvm"]
# Serving requires authentication configuration; see docs/docker.md.
CMD ["serve"]

# Optional troubleshooting image; these tools stay out of the default image.
FROM runtime-deps AS diagnostics
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg \
    && rm -rf /var/lib/apt/lists/*
USER 10001:10001

FROM runtime-deps AS rootfs
USER root
RUN <<'EOF'
set -eu
install -d -m 0755 /rootfs
install -d -m 1777 /rootfs/tmp
install -d -m 0700 -o 10001 -g 10001 /rootfs/var/lib/devbox-kvm

# The executable is static and embeds firmware; add certificates and user records.
cp --parents -L /usr/local/bin/devbox-kvm \
    /etc/ssl/certs/ca-certificates.crt /etc/passwd /etc/group /etc/nsswitch.conf \
    /etc/os-release /etc/debian_version /rootfs

# Keep attribution for the packaged runtime and the application's codecs.
for copyright in /usr/share/doc/*/copyright; do
    cp --parents -L "$copyright" /rootfs
done
cp -a /usr/share/common-licenses /rootfs/usr/share/
cp -a /usr/share/doc/devbox-gateway-kvm /rootfs/usr/share/doc/
dpkg-query -W > /rootfs/usr/share/doc/devbox-gateway-kvm/runtime-packages.txt

chroot --userspec=10001:10001 /rootfs /usr/local/bin/devbox-kvm serve -help
EOF

# LDAP initializes its own private identity; OpenSSL stays out of the gateway.
FROM glauth/glauth@sha256:6d448805cf54fc2ca3bca2b87f89dffe571a354a69a667b5a910111be3cf430e AS glauth
FROM debian:trixie-slim AS ldap
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates openssl \
    && rm -rf /var/lib/apt/lists/* \
    && install -d -m 0700 -o 10001 -g 10001 /app/config \
    && install -d -m 0755 -o 10001 -g 10001 /ldap-ca
COPY --from=glauth /app/glauth /app/glauth
RUN /app/glauth --version
COPY --chmod=0755 docker/start-ldap.sh /usr/local/bin/devbox-kvm-start-ldap
COPY testldap/default-config.cfg /usr/local/share/devbox-kvm-ldap/config.cfg
COPY testldap/LICENSE /usr/local/share/devbox-kvm-ldap/LICENSE
LABEL org.opencontainers.image.title="DevBox-Gateway-kvm LDAP" \
      org.opencontainers.image.source="https://github.com/define42/DevBox-Gateway-kvm" \
      org.opencontainers.image.description="Development LDAP directory with persistent TLS identity"
USER 10001:10001
EXPOSE 389/tcp
HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=5 \
    CMD openssl s_client -connect 127.0.0.1:389 -servername ldap \
        -verify_hostname ldap -verify_return_error -CAfile /app/config/cert.pem \
        </dev/null >/dev/null 2>&1
ENTRYPOINT ["/usr/local/bin/devbox-kvm-start-ldap"]

# Scratch keeps only the static Go application, certificates and licenses.
FROM scratch AS runtime
COPY --from=rootfs /rootfs/ /
LABEL org.opencontainers.image.title="DevBox-Gateway-kvm" \
      org.opencontainers.image.source="https://github.com/define42/DevBox-Gateway-kvm" \
      org.opencontainers.image.description="Pure Go KVM desktop gateway with 2D graphics and H.264 encoding" \
      com.define42.devbox.software-h264="purego"
ENV PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin \
    DATA_ROOT_DIR=/var/lib/devbox-kvm \
    LISTEN_ADDR=0.0.0.0:8443
USER 10001:10001
WORKDIR /var/lib/devbox-kvm
EXPOSE 8443/tcp
ENTRYPOINT ["/usr/local/bin/devbox-kvm"]
CMD ["serve"]
