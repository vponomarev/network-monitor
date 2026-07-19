#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "This test must run as root to start the eBPF collector" >&2
  exit 1
fi

binary=${1:-./netmon}
api_port=${NETMON_E2E_API_PORT:-19876}
source_port=${NETMON_E2E_SOURCE_PORT:-19877}
workdir=$(mktemp -d /tmp/netmon-metadata-refresh.XXXXXX)
netmon_pid=
source_pid=

cleanup() {
  if [[ -n ${netmon_pid} ]]; then
    kill "${netmon_pid}" 2>/dev/null || true
    wait "${netmon_pid}" 2>/dev/null || true
  fi
  if [[ -n ${source_pid} ]]; then
    kill "${source_pid}" 2>/dev/null || true
    wait "${source_pid}" 2>/dev/null || true
  fi
  rm -rf "${workdir}"
}
trap cleanup EXIT

mkdir -p "${workdir}/remote"
cat >"${workdir}/locations.yaml" <<'YAML'
locations:
  - network: 10.0.0.0/24
    location: test
YAML
cat >"${workdir}/roles.yaml" <<'YAML'
roles:
  - network: 10.0.0.1/32
    role: initial
YAML
cat >"${workdir}/remote/roles.yaml" <<'YAML'
roles:
  - network: 10.0.0.1/32
    role: refreshed
YAML
cat >"${workdir}/config.yaml" <<YAML
global:
  metrics_addr: 127.0.0.1
  metrics_port: ${api_port}
  auth_token: metadata-e2e-token
  loss_source: ebpf
metadata:
  locations:
    path: ${workdir}/locations.yaml
  roles:
    path: ${workdir}/roles.yaml
    update_source:
      url: http://127.0.0.1:${source_port}/roles.yaml
      poll_interval: 1h
      timeout: 5s
  unknown:
    enabled: false
topology:
  enabled: false
metrics:
  cardinality:
    level: role
    max_series: 100
discovery:
  traceroute:
    enabled: false
connections:
  enabled: false
logging:
  level: info
  format: json
YAML

python3 -m http.server "${source_port}" --bind 127.0.0.1 \
  --directory "${workdir}/remote" >"${workdir}/source.log" 2>&1 &
source_pid=$!

"${binary}" --config "${workdir}/config.yaml" >"${workdir}/netmon.log" 2>&1 &
netmon_pid=$!

for _ in $(seq 1 30); do
  if curl --silent --fail "http://127.0.0.1:${api_port}/ready" >/dev/null; then
    break
  fi
  if ! kill -0 "${netmon_pid}" 2>/dev/null; then
    cat "${workdir}/netmon.log" >&2
    exit 1
  fi
  sleep 1
done
curl --silent --fail "http://127.0.0.1:${api_port}/ready" >/dev/null

api_url="http://127.0.0.1:${api_port}/api/v1/metadata/refresh"
auth_header="Authorization: Bearer metadata-e2e-token"

curl --silent --show-error --fail-with-body -X POST \
  -H "${auth_header}" -H 'Content-Type: application/json' -d '{}' \
  "${api_url}" >"${workdir}/response.json"
grep -q '"status":"updated"' "${workdir}/response.json"
grep -q 'role: refreshed' "${workdir}/roles.yaml"
cp "${workdir}/roles.yaml" "${workdir}/expected-roles.yaml"

cat >"${workdir}/remote/roles.yaml" <<'YAML'
roles:
  - role: missing-network
YAML
status=$(curl --silent --show-error -o "${workdir}/response.json" \
  -w '%{http_code}' -X POST -H "${auth_header}" \
  -H 'Content-Type: application/json' -d '{"sources":["roles"]}' "${api_url}")
[[ ${status} == 500 ]]
grep -q 'validating roles metadata' "${workdir}/response.json"
grep -q 'network or networks is required' "${workdir}/response.json"
cmp "${workdir}/roles.yaml" "${workdir}/expected-roles.yaml"

cp "${workdir}/expected-roles.yaml" "${workdir}/remote/roles.yaml"
printf 'local drift\n' >"${workdir}/roles.yaml"
curl --silent --show-error --fail-with-body -X POST \
  -H "${auth_header}" -H 'Content-Type: application/json' \
  -d '{"sources":["roles"]}' "${api_url}" >"${workdir}/response.json"
cmp "${workdir}/roles.yaml" "${workdir}/expected-roles.yaml"

echo "METADATA_REFRESH_E2E_OK"
