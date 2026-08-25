# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -o /out/seedadmin ./cmd/seedadmin

FROM alpine:3.20
WORKDIR /app

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/server /out/seedadmin ./
COPY web ./web

ENV WEB_DIR=web
EXPOSE 8080

# CMD (not ENTRYPOINT) so `docker compose run app /app/seedadmin ...`
# actually replaces the command instead of appending to it.
CMD ["/app/server"]
