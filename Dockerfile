# syntax=docker/dockerfile:1

# Copyright (C) 2026 Joey Kot <joey.kot.x@gmail.com>
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.

FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

WORKDIR /src
COPY go.mod /src/go.mod
COPY cmd /src/cmd
COPY internal /src/internal

RUN --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    goarm="${TARGETVARIANT#v}"; \
    if [ "${TARGETARCH}" != "arm" ]; then goarm=""; fi; \
    CGO_ENABLED=0 \
    GOOS="${TARGETOS}" \
    GOARCH="${TARGETARCH}" \
    GOARM="${goarm}" \
    go build -trimpath -ldflags="-s -w" -o /out/kimi-compatible ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=build /out/kimi-compatible /usr/local/bin/kimi-compatible
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["docker-entrypoint.sh"]
