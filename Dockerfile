# Multi-stage Dockerfile for s3-sync

FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy source code
COPY . .

# Resolve modules, generate go.sum and download
RUN go mod tidy && go mod download

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /s3-sync main.go

# Minimal runtime image
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata bash nano curl

WORKDIR /workspace

# Copy compiled s3-sync binary into standard PATH
COPY --from=builder /s3-sync /usr/local/bin/s3-sync

# Copy default conf folder into image
COPY conf/ /workspace/conf/

# Default interactive shell
CMD ["/bin/sh"]
