#!/usr/bin/env bats
# kind requires a node's extraPortMapping containerPort to EQUAL the nodePort it
# forwards to, so kind-config.yaml and fixtures/nodeports/ duplicate every port
# number. Nothing at runtime reports a mismatch — the phase just gets connection
# refused and times out — so the coupling is asserted here instead.

bats_require_minimum_version 1.5.0

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert
  KIND_CONFIG="${BATS_TEST_DIRNAME}/kind-config.yaml"
  NODEPORTS_DIR="${BATS_TEST_DIRNAME}/fixtures/nodeports"
}

mapped_ports() {
  yq -oy '.nodes[0].extraPortMappings[].containerPort' "$KIND_CONFIG" | sort -n
}

service_nodeports() {
  # -N suppresses the `---` separators yq emits between input files.
  yq -N -oy 'select(.kind == "Service") | .spec.ports[].nodePort' \
    "$NODEPORTS_DIR"/*.yaml | sort -n
}

@test "every Service nodePort is mapped into the kind node" {
  run diff <(service_nodeports) <(mapped_ports)
  assert_success
}

@test "each mapped hostPort equals its containerPort" {
  # A mismatch here would publish the service on an unexpected localhost port, so
  # every URL in lib.sh would point at nothing.
  run yq -oy '[.nodes[0].extraPortMappings[] | select(.containerPort != .hostPort)] | length' "$KIND_CONFIG"
  assert_output "0"
}

@test "mappings are bound to loopback only" {
  # ArgoCD and Gitea run with throwaway admin credentials; a 0.0.0.0 binding would
  # expose them to the local network for the lifetime of the lab.
  run yq -oy '[.nodes[0].extraPortMappings[] | select(.listenAddress != "127.0.0.1")] | length' "$KIND_CONFIG"
  assert_output "0"
}

@test "every nodePort is unique" {
  total="$(service_nodeports | wc -l)"
  unique="$(service_nodeports | sort -u | wc -l)"
  assert_equal "$total" "$unique"
}

@test "lib.sh endpoint URLs use the published nodePorts" {
  # shellcheck source=./scripts/lib.sh
  . "${BATS_TEST_DIRNAME}/scripts/lib.sh"
  for url in "$AW_URL" "$AW_WS_URL" "$WHT_URL" "$GITEA_URL" "$ARGOCD_URL" "$GITOPS_REPO_URL" "$KEYCLOAK_URL"; do
    port="${url##*:}"; port="${port%%/*}"
    run grep -rqE "nodePort: ${port}\$" "$NODEPORTS_DIR"
    assert_success
  done
}
