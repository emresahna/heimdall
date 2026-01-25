# Heimdall
An observability platform for monitoring and managing distributed systems.

```bash
docker build -t heimdall-builder -f Dockerfile.builder .

docker run --rm \
    -v $(pwd):/app \
    heimdall-builder \
    protoc --go_out=. --go_opt=paths=source_relative \
            --go-grpc_out=. --go-grpc_opt=paths=source_relative \
            internal/sender/log.proto

docker run --rm \
    -v $(pwd):/app \
    heimdall-builder \
    go generate ./internal/collector
```