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

# Build the application
# We need CGO_ENABLED=1 because we use libvirt.org/go/libvirt
RUN CGO_ENABLED=1 GOOS=linux go build -o ec2-api ./cmd/api

# Stage 2: Final
FROM alpine:latest

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates libvirt-libs qemu-img

# Copy the binary from the builder stage
COPY --from=builder /app/ec2-api .

# Expose the application port
EXPOSE 8088

# Run the application
CMD ["./ec2-api"]
