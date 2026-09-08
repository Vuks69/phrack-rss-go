FROM golang:1.26-alpine3.23 AS build

WORKDIR /app

ENV CGO_ENABLED=0

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -trimpath -ldflags="-s -w" \
    -o /output/phrack-rss ./cmd/phrack-rss

FROM gcr.io/distroless/static-debian13

WORKDIR /app
USER nonroot

# Data + rendered feeds live here; mount a volume for persistence.
ENV PHRACK_RSS_DATA_DIR=/data
ENV PHRACK_RSS_LISTEN=127.0.0.1
ENV PHRACK_RSS_PORT=58315

COPY --from=build /output/phrack-rss /app/phrack-rss

EXPOSE 58315

CMD ["/app/phrack-rss"]