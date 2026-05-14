# Stage 1: Build
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git libvirt-dev gcc musl-dev pkgconf

# Copy go.mod and go.sum
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application (CGO required for libvirt)
RUN CGO_ENABLED=1 GOOS=linux go build -o ec2-api ./cmd

# Stage 2: Final runtime image
FROM alpine:latest

WORKDIR /app

# Runtime dependencies (IMPORTANT: openssh-client provides scp/ssh)
RUN apk add --no-cache \
    ca-certificates \
    libvirt-libs \
    qemu-img \
    cdrkit \
    openssh-client

# Copy binary + assets
COPY --from=builder /app/ec2-api .
COPY --from=builder /app/docs ./docs
COPY --from=builder /app/migrations ./migrations

# Expose API port
EXPOSE 8088

# Run application
CMD ["./ec2-api"]