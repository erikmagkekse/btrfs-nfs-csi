FROM golang:1.25-alpine AS build

ARG VERSION="dev"
ARG COMMIT="unknown"

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o btrfs-nfs-csi .

FROM alpine:3.21

RUN apk add --no-cache btrfs-progs e2fsprogs nfs-utils

COPY --from=build /build/btrfs-nfs-csi /usr/local/bin/btrfs-nfs-csi

ENTRYPOINT ["/usr/local/bin/btrfs-nfs-csi"]
