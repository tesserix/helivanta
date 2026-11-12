#!/usr/bin/env bash
set -euo pipefail

if [[ ${GITHUB_ACTIONS:-} != true || ${RUNNER_OS:-} != Linux ]]; then
  echo 'DNS recovery is limited to a GitHub Linux runner' >&2
  exit 1
fi

lookup() {
  timeout 10s getent ahostsv4 "$1" > /dev/null
}

healthy() {
  lookup archive.ubuntu.com && lookup security.ubuntu.com
}

if healthy; then
  echo 'Public dependency DNS is ready.'
  exit 0
fi

runner_iface=$(ip -o route show default | awk '{for (i=1;i<=NF;i++) if ($i=="dev") {print $(i+1); exit}}')
if [[ ! $runner_iface =~ ^[a-zA-Z0-9][a-zA-Z0-9_.:-]*$ ]]; then
  echo 'Could not identify the CI default network interface' >&2
  exit 1
fi

sudo resolvectl dns "$runner_iface" 1.1.1.1 8.8.8.8
sudo resolvectl flush-caches
if ! healthy; then
  echo 'Public dependency DNS remains unavailable after CI recovery' >&2
  exit 1
fi
echo 'Public dependency DNS recovered.'
