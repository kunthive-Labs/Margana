# syntax=docker/dockerfile:1

# --- build stage: static, CGO-free binary ------------------------------------
FROM golang:1.25-alpine AS build

# Use the image's toolchain; go.mod requires 1.25 and the image satisfies it.
ENV GOTOOLCHAIN=local
WORKDIR /src

# Cache module downloads across rebuilds.
COPY go.mod go.sum ./
RUN go mod download

# modernc.org/sqlite is pure Go, so CGO stays off and the result is a fully
# static binary that runs on a scratch/alpine base with no libc. -buildvcs=false
# keeps the build independent of git (the .git dir is not in the build context).
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/relay ./cmd/relay

# --- runtime stage -----------------------------------------------------------
FROM alpine:3.20

# A fresh named volume mounted at /data inherits this directory's ownership, so
# the non-root user can write the SQLite database.
RUN adduser -D -H -u 10001 relay \
	&& mkdir -p /data \
	&& chown relay /data

COPY --from=build /out/relay /usr/local/bin/relay

USER relay
WORKDIR /data
VOLUME ["/data"]

ENV LISTEN_ADDR=:8443 \
	RELAY_DB_PATH=/data/relay.db \
	RELAY_RETENTION=0 \
	RELAY_BACKEND=local

EXPOSE 8443

# /healthz is exempt from the API key, so this works with or without API_KEY.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
	CMD wget -q -O- http://127.0.0.1:8443/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/relay"]
