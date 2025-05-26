# Build stage
FROM golang:1.24-alpine AS builder

# Set working directory
WORKDIR /app

# Install git, Node.js, npm and other dependencies
RUN apk add --no-cache git ca-certificates tzdata nodejs npm

# Copy go.mod and go.sum first (better layer caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy package.json and package-lock.json for CSS dependencies
COPY static/css/package*.json ./static/css/
RUN cd static/css && npm ci

# Copy the source code and config template
COPY . .

# Build CSS
RUN cd static/css && npm run build

# Generate config file from template
RUN cp config.yaml.template config.yaml

# Build the application with version information
ARG VERSION=dev
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags "-extldflags '-static' -X github.com/kuhlman-labs/gh-ghes-2-ghec/internal/version.Version=${VERSION} -X github.com/kuhlman-labs/gh-ghes-2-ghec/internal/version.BuildTime=${BUILD_TIME}" -o gh-ghes-2-ghec main.go

# Final lightweight runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

# Copy binary and config files from the builder stage
COPY --from=builder /app/gh-ghes-2-ghec /app/gh-ghes-2-ghec
COPY --from=builder /app/config.yaml /app/config.yaml
COPY --from=builder /app/static /app/static
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Set the working directory
WORKDIR /app

# Run as non-root user
USER nonroot:nonroot

# Expose the port the server listens on
EXPOSE 8080

# Command to run the executable
ENTRYPOINT ["/app/gh-ghes-2-ghec"] 