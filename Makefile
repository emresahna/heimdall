.PHONY: help build build-agent build-server docker-agent docker-server manifests \
        generate generate-proto generate-ebpf generate-ebpf-docker generate-vmlinux \
        builder-image clean

PROTO_FILE := internal/sender/log.proto
BPF_DIR := internal/bpf
VMLINUX_FILE := $(BPF_DIR)/vmlinux.h
BUILDER_IMAGE := heimdall-builder

help:
	@echo "Available targets:"
	@echo "  make build               Build local agent and server binaries"
	@echo "  make generate            Generate proto and eBPF artifacts"
	@echo "  make generate-proto      Regenerate gRPC/protobuf Go files"
	@echo "  make generate-ebpf       Regenerate eBPF bindings/object (needs Linux + toolchain)"
	@echo "  make generate-ebpf-docker Regenerate eBPF bindings/object in a container (macOS-friendly)"
	@echo "  make builder-image       Build the toolchain image (Dockerfile.builder)"
	@echo "  make docker-agent        Build heimdall-agent Docker image"
	@echo "  make docker-server       Build heimdall-server Docker image"
	@echo "  make manifests           Apply Kubernetes manifests"

build: build-agent build-server

build-agent:
	CGO_ENABLED=0 go build -o bin/agent ./cmd/agent

build-server:
	CGO_ENABLED=0 go build -o bin/server ./cmd/server

generate: generate-proto generate-ebpf

generate-proto:
	@command -v protoc >/dev/null 2>&1 || { echo "protoc is required"; exit 1; }
	@command -v protoc-gen-go >/dev/null 2>&1 || { echo "protoc-gen-go is required"; exit 1; }
	@command -v protoc-gen-go-grpc >/dev/null 2>&1 || { echo "protoc-gen-go-grpc is required"; exit 1; }
	protoc --proto_path=. \
	  --go_out=. --go_opt=paths=source_relative \
	  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	  $(PROTO_FILE)

generate-vmlinux:
	@command -v bpftool >/dev/null 2>&1 || { echo "bpftool is required"; exit 1; }
	bpftool btf dump file /sys/kernel/btf/vmlinux format c > $(VMLINUX_FILE)

generate-ebpf: generate-vmlinux
	@command -v bpf2go >/dev/null 2>&1 || { echo "bpf2go is required"; exit 1; }
	GOOS=linux GOARCH=amd64 go generate ./internal/bpf/...

builder-image:
	@docker image inspect $(BUILDER_IMAGE) >/dev/null 2>&1 || \
		docker build -t $(BUILDER_IMAGE) -f Dockerfile.builder .

generate-ebpf-docker: builder-image
	docker run --rm -v $(CURDIR):/work -w /work $(BUILDER_IMAGE) bash -c \
		'bpftool btf dump file /sys/kernel/btf/vmlinux format c > internal/bpf/vmlinux.h && \
		cd internal/bpf && \
		GOPACKAGE=bpf GOARCH=amd64 bpf2go -target bpf Tracker tracker.c -- -I -O2 -g0'

docker-agent:
	docker build -t heimdall-agent:latest -f Dockerfile.agent .

docker-server:
	docker build -t heimdall-server:latest -f Dockerfile.server .

manifests:
	kubectl apply -f deploy/k8s/clickhouse.yaml
	kubectl apply -f deploy/k8s/server-deployment.yaml
	kubectl apply -f deploy/k8s/agent-rbac.yaml
	kubectl apply -f deploy/k8s/agent-ds.yaml

clean:
	rm -rf bin
