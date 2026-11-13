#!/usr/bin/env bash
set -euo pipefail
[[ $# == 1 ]]
curl --fail --silent --show-error --location \
  https://raw.githubusercontent.com/tesserix/tesserix-k8s/b14f6f815351a3efc30a39e360023fe37a9c3d3d/charts/apps/db-schema-bootstrap/schemas/global/global/onboarding_db.sql \
  --output "$1"
printf '%s  %s\n' 117bf30c7e4c32dddcd7cdb119a1812a75a9f91d8c5958cf0c950b0bbd2f8258 "$1" | shasum -a 256 -c
