# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o worker ./cmd/worker

FROM gcr.io/distroless/base-debian12

WORKDIR /app
COPY --from=builder /app/worker /usr/local/bin/worker

ENTRYPOINT ["/usr/local/bin/worker"]
