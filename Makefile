.PHONY: build build-agent build-server docker-agent docker-server

build: build-agent build-server

build-agent:
	@if [ ! -f internal/collector/vmlinux.h ]; then \
		echo "vmlinux.h missing, attempting to generate via Docker..."; \
		docker run --privileged --rm -v $(pwd):/app golang:1.25.3-bookworm sh -c "apt-get update && apt-get install -y bpftool && bpftool vmlinux dump run > /app/internal/collector/vmlinux.h" || touch internal/collector/vmlinux.h; \
	fi
	docker build -t heimdall-agent:latest -f Dockerfile.agent .
	docker run --rm -v $(pwd):/app heimdall-agent:latest go generate ./internal/collector/...
	go build -o bin/agent ./cmd/agent

build-server:
	go build -o bin/server ./cmd/server

docker-agent:
	docker build -t heimdall-agent:latest -f deploy/docker/Dockerfile.agent .

docker-server:
	docker build -t heimdall-server:latest -f deploy/docker/Dockerfile.server .

manifests:
	kubectl apply -f deploy/k8s/
