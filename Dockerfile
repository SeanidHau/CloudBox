FROM golang:1.26.5-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/cloudbox \
    ./cmd/api

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install --no-install-recommends --yes ffmpeg \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system cloudbox \
    && useradd --system --gid cloudbox --home-dir /app cloudbox

WORKDIR /app

COPY --from=build /out/cloudbox /usr/local/bin/cloudbox
COPY --from=build /src/migrations ./migrations

RUN mkdir -p /app/uploads /data \
    && chown -R cloudbox:cloudbox /app /data

USER cloudbox

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/cloudbox"]
