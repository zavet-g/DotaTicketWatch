FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN GOPROXY=https://goproxy.io,direct go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o bot ./cmd/bot

FROM alpine:3.21

RUN apk add --no-cache curl ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/bot .

VOLUME ["/app/data"]

CMD ["./bot"]
