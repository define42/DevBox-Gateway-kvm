# syntax=docker/dockerfile:1

# Keep the builder and runtime on the same Debian release for FFmpeg's ABI.
FROM golang:1.26-trixie AS build

RUN test "$(dpkg --print-architecture)" = amd64 \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        libavcodec-dev libavutil-dev libvirglrenderer-dev libegl-dev libgbm-dev pkg-config nasm \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Native OpenH264 with NASM assembly is the image's software H.264 encoder.
COPY tools/build-openh264.sh ./tools/
RUN tools/build-openh264.sh

COPY *.go ./
COPY internal/ ./internal/
COPY kvm/ ./kvm/
COPY pkg/ ./pkg/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -tags nvenc,virgl -trimpath -buildvcs=false \
        -ldflags="-s -w" -o /out/devbox-kvm .

FROM debian:trixie-slim AS runtime-deps

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates libavcodec61 libavutil59 ovmf \
        libvirglrenderer1 libegl1 libegl-mesa0 libgl1 libgles2 libgl1-mesa-dri \
    && rm -rf /var/lib/apt/lists/* \
    && test -r /usr/share/OVMF/OVMF_CODE_4M.fd \
    && test -r /usr/share/OVMF/OVMF_VARS_4M.fd \
    && groupadd --gid 10001 devbox \
    && useradd --uid 10001 --gid 10001 --no-create-home \
        --home-dir /var/lib/devbox-kvm --shell /usr/sbin/nologin devbox \
    && install -d -m 0700 -o devbox -g devbox /var/lib/devbox-kvm

COPY --from=build /out/devbox-kvm /usr/local/bin/devbox-kvm
COPY LICENSE LICENSE-OpenH264 LICENSE-go.264 /usr/share/doc/devbox-gateway-kvm/
COPY internal/qcow2import/LICENSE /usr/share/doc/devbox-gateway-kvm/LICENSE-go-qcow2reader

# The NVIDIA Container Toolkit injects the host driver when --gpus is supplied.
ENV NVIDIA_DRIVER_CAPABILITIES=compute,video,utility \
    DATA_ROOT_DIR=/var/lib/devbox-kvm \
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

# ldd includes transitive dependencies and the ELF interpreter. Dereference
# symlinks so the copied files do not depend on Debian's merged /usr layout.
# EGL vendor dispatch and Mesa drivers are loaded dynamically, so ldd on the
# executable alone misses them. Include every copied driver and its dependencies.
ldd /usr/local/bin/devbox-kvm \
    /usr/lib/x86_64-linux-gnu/libEGL.so.1 /usr/lib/x86_64-linux-gnu/libEGL_mesa.so.0 \
    /usr/lib/x86_64-linux-gnu/libGL.so.1 /usr/lib/x86_64-linux-gnu/libGLESv2.so.2 \
    /usr/lib/x86_64-linux-gnu/libgbm.so.1 /usr/lib/x86_64-linux-gnu/dri/*.so \
    > /tmp/runtime-libraries
if grep -q 'not found' /tmp/runtime-libraries; then
    cat /tmp/runtime-libraries
    exit 1
fi
awk '$1 ~ /^\// && $2 ~ /^\(/ { print $1 } $3 ~ /^\// { print $3 }' /tmp/runtime-libraries \
    | sort -u > /tmp/runtime-library-paths
while IFS= read -r library; do
    cp --parents -L "$library" /rootfs
done < /tmp/runtime-library-paths

# NVIDIA's injected driver libraries can depend on these compatibility DSOs
# even when the gateway's own glibc dependencies no longer list them.
cp --parents -L /lib/x86_64-linux-gnu/libdl.so.2 \
    /lib/x86_64-linux-gnu/libpthread.so.0 /lib/x86_64-linux-gnu/librt.so.1 /rootfs
cp --parents -L /usr/local/bin/devbox-kvm /sbin/ldconfig \
    /usr/share/OVMF/OVMF_CODE_4M.fd /usr/share/OVMF/OVMF_VARS_4M.fd \
    /etc/ssl/certs/ca-certificates.crt /etc/passwd /etc/group /etc/nsswitch.conf \
    /etc/os-release /etc/debian_version /etc/ld.so.conf /rootfs
cp -a /etc/ld.so.conf.d /rootfs/etc/
install -d /rootfs/usr/lib/x86_64-linux-gnu /rootfs/usr/local/lib
cp --parents -L /usr/lib/x86_64-linux-gnu/libEGL.so.1 \
    /usr/lib/x86_64-linux-gnu/libEGL_mesa.so.0 /usr/lib/x86_64-linux-gnu/libGL.so.1 \
    /usr/lib/x86_64-linux-gnu/libGLESv2.so.2 /usr/lib/x86_64-linux-gnu/libgbm.so.1 /rootfs
cp -aL /usr/lib/x86_64-linux-gnu/dri /rootfs/usr/lib/x86_64-linux-gnu/
cp -a --parents /usr/share/glvnd/egl_vendor.d /usr/share/drirc.d /rootfs

# Keep attribution for the packaged runtime and the application's codecs.
for copyright in /usr/share/doc/*/copyright; do
    cp --parents -L "$copyright" /rootfs
done
cp -a /usr/share/common-licenses /rootfs/usr/share/
cp -a /usr/share/doc/devbox-gateway-kvm /rootfs/usr/share/doc/
dpkg-query -W > /rootfs/usr/share/doc/devbox-gateway-kvm/runtime-packages.txt

# NVIDIA's runtime uses the Debian marker and loader configuration when adding
# host driver libraries. Build a cache for this filesystem, not the build stage.
ldconfig -r /rootfs
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

# Scratch removes the distribution tools, not NVENC's dynamic dependencies.
FROM scratch AS runtime
COPY --from=rootfs /rootfs/ /
LABEL org.opencontainers.image.title="DevBox-Gateway-kvm" \
      org.opencontainers.image.source="https://github.com/define42/DevBox-Gateway-kvm" \
      org.opencontainers.image.description="Minimal KVM desktop gateway with optional guest OpenGL and NVIDIA NVENC encoding"
ENV PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin \
    NVIDIA_DRIVER_CAPABILITIES=compute,video,utility \
    DATA_ROOT_DIR=/var/lib/devbox-kvm \
    LISTEN_ADDR=0.0.0.0:8443
USER 10001:10001
WORKDIR /var/lib/devbox-kvm
EXPOSE 8443/tcp
ENTRYPOINT ["/usr/local/bin/devbox-kvm"]
CMD ["serve"]
