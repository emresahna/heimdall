#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLUSTER_NAME="${CLUSTER_NAME:-heimdall-local}"
KIND_CONFIG="${KIND_CONFIG:-${ROOT_DIR}/kind-config.yaml}"
CLICKHOUSE_TIMEOUT="${CLICKHOUSE_TIMEOUT:-240s}"
SERVER_TIMEOUT="${SERVER_TIMEOUT:-240s}"
AGENT_TIMEOUT="${AGENT_TIMEOUT:-240s}"

ACTION="${1:-run}"
EXPECTED_CONTEXT="kind-${CLUSTER_NAME}"

require_commands() {
  local missing=()
  local cmd
  for cmd in docker kind kubectl; do
    if ! command -v "${cmd}" >/dev/null 2>&1; then
      missing+=("${cmd}")
    fi
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    echo "Missing required commands: ${missing[*]}"
    exit 1
  fi
}

require_kind_context() {
  local current_context
  current_context="$(kubectl config current-context 2>/dev/null || true)"
  if [[ "${current_context}" != "${EXPECTED_CONTEXT}" ]]; then
    echo "Current kubectl context is '${current_context}'. Switch to '${EXPECTED_CONTEXT}' first."
    echo "Run: kubectl config use-context ${EXPECTED_CONTEXT}"
    exit 1
  fi
}

create_cluster() {
  if kind get clusters | grep -Fxq "${CLUSTER_NAME}"; then
    echo "Kind cluster '${CLUSTER_NAME}' already exists."
    return
  fi

  kind create cluster --name "${CLUSTER_NAME}" --config "${KIND_CONFIG}"
  kubectl cluster-info
  kubectl get nodes -o wide
}

build_and_load_images() {
  if ! kind get clusters | grep -Fxq "${CLUSTER_NAME}"; then
    echo "Kind cluster '${CLUSTER_NAME}' not found. Create it first."
    exit 1
  fi

  cd "${ROOT_DIR}"
  docker build --no-cache -t heimdall-agent:latest -f Dockerfile.agent .
  docker build --no-cache -t heimdall-server:latest -f Dockerfile.server .

  kind load docker-image --name "${CLUSTER_NAME}" heimdall-agent:latest
  kind load docker-image --name "${CLUSTER_NAME}" heimdall-server:latest
  echo "Images built and loaded into kind/${CLUSTER_NAME}."
}

deploy_stack() {
  require_kind_context

  cd "${ROOT_DIR}"
  kubectl apply -f deploy/k8s/clickhouse.yaml
  kubectl apply -f deploy/k8s/server-deployment.yaml
  kubectl apply -f deploy/k8s/agent-rbac.yaml
  kubectl apply -f deploy/k8s/agent-ds.yaml

  kubectl rollout status statefulset/clickhouse -n default --timeout="${CLICKHOUSE_TIMEOUT}"
  kubectl rollout status deployment/heimdall-server -n default --timeout="${SERVER_TIMEOUT}"
  kubectl rollout status daemonset/heimdall-agent -n default --timeout="${AGENT_TIMEOUT}"

  echo "All workloads are rolled out."
  kubectl get pods -n default -o wide
}

cleanup_stack() {
  require_kind_context

  cd "${ROOT_DIR}"
  kubectl delete -f deploy/k8s/agent-ds.yaml --ignore-not-found=true
  kubectl delete -f deploy/k8s/agent-rbac.yaml --ignore-not-found=true
  kubectl delete -f deploy/k8s/server-deployment.yaml --ignore-not-found=true
  kubectl delete -f deploy/k8s/clickhouse.yaml --ignore-not-found=true

  if [[ "${DELETE_CLUSTER:-false}" == "true" ]]; then
    kind delete cluster --name "${CLUSTER_NAME}"
  fi

  echo "Kubernetes resources cleaned up."
}

print_usage() {
  cat <<'EOF'
Usage:
  ./local_test.sh [run|prereqs|cluster|build|deploy|cleanup]

Actions:
  run      Full local flow (default): prereqs -> cluster -> build -> deploy
  prereqs  Verify required local tools
  cluster  Create kind cluster
  build    Build and load Docker images into kind
  deploy   Apply Kubernetes manifests and wait for rollout
  cleanup  Delete Kubernetes resources (set DELETE_CLUSTER=true to delete cluster)
EOF
}

case "${ACTION}" in
  run)
    require_commands
    create_cluster
    build_and_load_images
    deploy_stack
    ;;
  prereqs)
    require_commands
    echo "Required commands are installed."
    docker version >/dev/null
    kind version >/dev/null
    kubectl version --client >/dev/null
    echo "Local tooling looks ready."
    ;;
  cluster)
    require_commands
    create_cluster
    ;;
  build)
    require_commands
    build_and_load_images
    ;;
  deploy)
    require_commands
    deploy_stack
    ;;
  cleanup)
    require_commands
    cleanup_stack
    ;;
  *)
    print_usage
    exit 1
    ;;
esac
