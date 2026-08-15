.PHONY: help build build-agent build-server docker-agent docker-server manifests \
        generate generate-proto generate-ebpf generate-ebpf-docker generate-vmlinux \
        builder-image helm-lint helm-template changelog clean

PROTO_FILE := internal/sender/log.proto
BPF_DIR := internal/bpf
VMLINUX_FILE := $(BPF_DIR)/vmlinux.h
BUILDER_IMAGE := heimdall-builder
VERSION ?= dev
REVISION ?= $(shell git rev-parse --short HEAD)
HELM ?= helm
HELM_RELEASE ?= heimdall
HELM_NAMESPACE ?= default

help:
	@echo "Available targets:"
	@echo "  make build               Build local agent and server binaries (VERSION=dev)"
	@echo "  make generate            Generate proto and eBPF artifacts"
	@echo "  make generate-proto      Regenerate gRPC/protobuf Go files"
	@echo "  make generate-ebpf       Regenerate eBPF bindings/object (needs Linux + toolchain)"
	@echo "  make generate-ebpf-docker Regenerate eBPF bindings/object in a container (macOS-friendly)"
	@echo "  make builder-image       Build the toolchain image (Dockerfile.builder)"
	@echo "  make docker-agent        Build heimdall-agent Docker image"
	@echo "  make docker-server       Build heimdall-server Docker image"
	@echo "  make helm-lint           Lint the Helm chart"
	@echo "  make helm-template       Render the Helm chart"
	@echo "  make changelog           Regenerate CHANGELOG.md (requires git-cliff)"
	@echo "  make manifests           Apply Kubernetes manifests"

build: build-agent build-server

build-agent:
	CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o bin/agent ./cmd/agent

build-server:
	CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o bin/server ./cmd/server

generate: generate-vmlinux generate-ebpf generate-proto

generate-vmlinux:
	@command -v bpftool >/dev/null 2>&1 || { echo "bpftool is required"; exit 1; }
	bpftool btf dump file /sys/kernel/btf/vmlinux format c > $(VMLINUX_FILE)

generate-ebpf:
	@command -v bpf2go >/dev/null 2>&1 || { echo "bpf2go is required"; exit 1; }
	GOPACKAGE=bpf GOARCH=amd64 go generate ./internal/bpf/...

generate-proto:
	@command -v protoc >/dev/null 2>&1 || { echo "protoc is required"; exit 1; }
	@command -v protoc-gen-go >/dev/null 2>&1 || { echo "protoc-gen-go is required"; exit 1; }
	@command -v protoc-gen-go-grpc >/dev/null 2>&1 || { echo "protoc-gen-go-grpc is required"; exit 1; }
	protoc --proto_path=. \
	  --go_out=. --go_opt=paths=source_relative \
	  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	  $(PROTO_FILE)

builder-image:
	@docker image inspect $(BUILDER_IMAGE) >/dev/null 2>&1 || \
		docker build -t $(BUILDER_IMAGE) -f Dockerfile.builder .

generate-docker: generate-header-docker generate-ebpf-docker generate-proto-docker

generate-header-docker: builder-image
	docker run --rm -v $(CURDIR):/app $(BUILDER_IMAGE) \
		bpftool btf dump file /sys/kernel/btf/vmlinux format c > $(VMLINUX_FILE)

generate-ebpf-docker: builder-image
	docker run --rm -v $(CURDIR):/app -e GOPACKAGE=bpf -e GOARCH=amd64 $(BUILDER_IMAGE) \
		bpf2go -target bpf -output-dir internal/bpf Tracker internal/bpf/tracker.c -- -I -O2 -g0

generate-proto-docker: builder-image
	docker run --rm -v $(CURDIR):/app $(BUILDER_IMAGE) \
		protoc --proto_path=. --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative $(PROTO_FILE)

build-docker: docker-agent docker-server

docker-agent:
	docker build --build-arg VERSION=$(VERSION) --build-arg REVISION=$(REVISION) -t heimdall-agent:$(VERSION) -f Dockerfile.agent .

docker-server:
	docker build --build-arg VERSION=$(VERSION) --build-arg REVISION=$(REVISION) -t heimdall-server:$(VERSION) -f Dockerfile.server .

helm-lint:
	$(HELM) lint helm

helm-template:
	$(HELM) template $(HELM_RELEASE) helm --namespace $(HELM_NAMESPACE)

changelog:
	@command -v git-cliff >/dev/null 2>&1 || { echo "git-cliff is required"; exit 1; }
	git-cliff --config cliff.toml --output CHANGELOG.md

manifests:
	kubectl apply -f deploy/k8s/clickhouse.yaml
	kubectl apply -f deploy/k8s/server-deployment.yaml
	kubectl apply -f deploy/k8s/agent-rbac.yaml
	kubectl apply -f deploy/k8s/agent-ds.yaml

clean:
	rm -rf bin
