FROM golang:1.25.3-bookworm

RUN apt-get update && apt-get install -y \
    clang \
    llvm \
    libbpf-dev \
    gcc \
    make \
    git

WORKDIR /app

RUN ln -s /usr/include/$(uname -m)-linux-gnu/asm /usr/include/asm