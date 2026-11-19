#!/usr/bin/env bash
set -euo pipefail
[[ $# == 1 ]]
curl --fail --silent --show-error --location \
  https://raw.githubusercontent.com/tesserix/tesserix-k8s/7bd817e6fdfec553ee95499faf6076f155a4907d/charts/apps/db-schema-bootstrap/schemas/global/global/onboarding_db.sql \
  --output "$1"
printf '%s  %s\n' 844877964546ef1c936298837123c6f528543d8ca609effb4aa7fe566ab6b0a2 "$1" | shasum -a 256 -c
