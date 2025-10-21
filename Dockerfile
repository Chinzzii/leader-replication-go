# --- Builder Stage ---
# Use the official Golang image as a builder.
# Pinning to a specific version (1.22) ensures reproducible builds.
FROM golang:1.23-alpine AS builder

WORKDIR /app

# **OPTIMIZATION:** Cache dependencies.
# Copy mod/sum files first.
COPY go.mod go.sum ./
# Download dependencies. This layer is only rebuilt if mod/sum files change.
RUN go mod download

# Copy the rest of the source code.
COPY internal ./internal
COPY cmd ./cmd

# **OPTIMIZATION:** Build a static, CGO-disabled binary.
# This is crucial for running in a minimal container (like alpine)
# that doesn't have the C libraries Go links against by default.
# Use -trimpath to remove build path info and -ldflags="-s -w" to strip debug info.
RUN CGO_ENABLED=0 go build \
    -o /bin/replica \
    -trimpath \
    -ldflags="-s -w" \
    ./cmd/server


# --- Final Stage ---
# Use a minimal, secure base image. Alpine is a great choice.
FROM alpine:latest

# We need ca-certificates for making HTTPS calls (if any peers are HTTPS)
# and tzdata for correct time/date logging.
RUN apk add --no-cache ca-certificates tzdata

# Copy *only* the compiled binary from the builder stage.
COPY --from=builder /bin/replica /bin/replica

# Expose the ports used by the leader and followers in the docker-compose file.
EXPOSE 8080 8081 8082

# Set the binary as the entrypoint.
# Command-line args will be appended by docker-compose.
ENTRYPOINT ["/bin/replica"]
