FROM golang:1.25-alpine AS build

ARG VERSION="dev"
ARG COMMIT="unknown"

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o btrfs-nfs-csi ./cmd/btrfs-nfs-csi

FROM alpine:3.21

LABEL org.opencontainers.image.title="btrfs-nfs-csi" \
      org.opencontainers.image.description="Kubernetes CSI driver that turns any btrfs filesystem into a full-featured NFS storage backend with instant snapshots, clones, and quotas" \
      org.opencontainers.image.url="https://github.com/erikmagkekse/btrfs-nfs-csi" \
      org.opencontainers.image.source="https://github.com/erikmagkekse/btrfs-nfs-csi" \
      org.opencontainers.image.documentation="https://github.com/erikmagkekse/btrfs-nfs-csi#readme" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.vendor="Erik Groh <me@eriks.life>"

RUN apk add --no-cache btrfs-progs e2fsprogs nfs-utils

COPY --from=build /build/btrfs-nfs-csi /usr/local/bin/btrfs-nfs-csi

ENTRYPOINT ["/usr/local/bin/btrfs-nfs-csi"]
