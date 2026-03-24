# Build stage
FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o cyntr ./cmd/cyntr

# Runtime stage
FROM alpine:3.21
RUN apk add --no-cache ca-certificates bash
WORKDIR /app

COPY --from=builder /app/cyntr /usr/local/bin/cyntr

# Create data directory
RUN mkdir -p /data

# Default config
ENV CYNTR_DATA_DIR=/data

EXPOSE 7700 8080 3000

ENTRYPOINT ["cyntr"]
CMD ["start"]
