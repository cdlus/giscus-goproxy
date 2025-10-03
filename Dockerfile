# -------- Build stage --------
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Install git (for go modules) and CA certs
RUN apk add --no-cache git ca-certificates && update-ca-certificates

# Pre-cache go modules
COPY go.mod ./
RUN go mod download

# Copy source
COPY . .

# Build static binary
# Honors TARGETOS/TARGETARCH when provided (e.g., via buildx); defaults to linux/amd64.
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64}
RUN go build -ldflags='-s -w' -o /out/giscus-wrapper ./


# -------- Runtime stage --------
FROM gcr.io/distroless/static:nonroot

WORKDIR /

# Copy CA certs and binary
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/giscus-wrapper /giscus-wrapper

# Run as non-root for security
USER nonroot:nonroot

# Tell Docker/Vercel the app can serve on 8080 (just metadata)
EXPOSE 8080

# Start the binary
ENTRYPOINT ["/giscus-proxy"]
