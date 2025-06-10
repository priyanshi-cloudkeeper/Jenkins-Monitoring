# ---- Stage 1: Build the Go application ----
# Use an official Go image as the builder.
# Using a specific version is good practice.
FROM golang:1.23-alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy the Go module files and download dependencies.
# This is done first to leverage Docker's layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application source code
COPY . .

# Build the Go application.
# -o specifies the output file name.
# CGO_ENABLED=0 creates a static binary, which is great for containers.
RUN CGO_ENABLED=0 go build -o /jenkins-dashboard .


# ---- Stage 2: Create the final, minimal image ----
# Use a minimal base image like Alpine Linux for a small footprint.
FROM alpine:latest

# Set the working directory
WORKDIR /app

# Copy the compiled binary from the 'builder' stage
COPY --from=builder /jenkins-dashboard .

# Copy the static files (HTML, CSS) needed by the application at runtime
COPY static ./static

# Expose the port the application listens on
EXPOSE 9090

# The command to run when the container starts
CMD ["./jenkins-dashboard"]