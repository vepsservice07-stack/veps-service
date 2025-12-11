# Build stage
FROM golang:1.24-alpine AS builder

# Install protoc and protoc-gen-go
RUN apk add --no-cache protobuf-dev git

WORKDIR /app

# Install protoc plugins first
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Copy proto file and generate code FIRST
COPY api/proto/ledger.proto api/proto/
RUN mkdir -p pkg/ledger

# Generate directly into pkg/ledger with correct module path
RUN protoc --go_out=. --go_opt=module=github.com/veps-service-480701/monolith-submitter \
    --go-grpc_out=. --go-grpc_opt=module=github.com/veps-service-480701/monolith-submitter \
    api/proto/ledger.proto

# Now copy go mod files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy rest of source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o monolith-submitter ./cmd/server

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/monolith-submitter .

# Expose port
EXPOSE 8080

# Run the binary
CMD ["./monolith-submitter"]
