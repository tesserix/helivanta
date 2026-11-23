#!/usr/bin/env bash

validate_e2e_spec_filter() {
  local repo_root=$1 selected_spec=${2-}
  [ -z "$selected_spec" ] && return 0
  if [[ ! "$selected_spec" =~ ^[a-z0-9][a-z0-9-]*\.spec\.ts$ ]] ||
    [ ! -f "$repo_root/e2e/tests/$selected_spec" ]; then
    echo "Focused E2E selection must name one existing specs-project *.spec.ts file." >&2
    return 2
  fi
}
