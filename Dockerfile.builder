FROM golang:1.25.3-bookworm

RUN apt-get update && apt-get install -y \
    clang \
    llvm \
    libbpf-dev \
    gcc \
    make \
    git \
    protobuf-compiler

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

WORKDIR /app

RUN ln -s /usr/include/$(uname -m)-linux-gnu/asm /usr/include/asm