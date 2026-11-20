FROM golang:1.27.3-alpine AS builder
WORKDIR /app
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o jhb-auto ./cmd/api

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/jhb-auto ./jhb-auto
EXPOSE 9105
CMD ["./jhb-auto"]
