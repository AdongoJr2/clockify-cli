# syntax=docker/dockerfile:1

# ---- build stage -----------------------------------------------------------
FROM golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build

ARG VERSION=dev
WORKDIR /src

# Dependencies first so edits to the source don't bust the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Static build: no libc dependency, so it runs on a bare Alpine image.
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/AdongoJr2/clockify-cli/cmd.version=${VERSION}" \
      -o /out/clockify .

# ---- runtime stage ---------------------------------------------------------
# NOTE: pinned to the tag, not a digest — Docker Hub does not serve alpine's
# OCI image-index digest by reference (content-negotiation-only), so a digest
# pin here fails to resolve in BuildKit. The golang build-stage pin above
# *is* digest-pinned. Dependabot keeps this tag current.
FROM alpine:3.24

# tzdata: the CLI resolves local timezones; ca-certificates: TLS to api.clockify.me
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 -h /home/clockify clockify

COPY --from=build /out/clockify /usr/local/bin/clockify

# Config and cache live in volumes so credentials survive container restarts.
ENV XDG_CONFIG_HOME=/config \
    XDG_CACHE_HOME=/cache \
    HOME=/home/clockify

RUN mkdir -p /config /cache \
    && chown -R clockify:clockify /config /cache /home/clockify

USER clockify
VOLUME ["/config", "/cache"]

ENTRYPOINT ["clockify"]
CMD ["--help"]