#!/usr/bin/env bash
# Copyright 2026 The KubeStack Authors. All rights reserved.
#
# Fails on forbidden licenses (CLAUDE.md rule 11) in anything shipped: Go deps of cmd/... and
# production npm deps of the portal. LGPL/EPL/source-available need an entry in compliance/APPROVALS.md.
# shortcut: text matching on LICENSE files, not SPDX analysis; switch to a scanner (e.g. go-licenses) when CI exists.
set -euo pipefail
cd "$(dirname "$0")/.."

forbidden='GNU AFFERO|GNU GENERAL PUBLIC LICENSE|Server Side Public License|Business Source License|Commons Clause'
needs_approval='GNU LESSER|Eclipse Public License'
fail=0

check() { # name, license text
  local name=$1 text=$2
  if grep -qiE "$needs_approval" <<<"$text"; then
    grep -qF "$name" compliance/APPROVALS.md || { echo "NEEDS APPROVAL: $name"; fail=1; }
  elif grep -qiE "$forbidden" <<<"$text"; then
    echo "FORBIDDEN: $name"; fail=1
  fi
}

while read -r mod dir; do
  [[ $mod == kubestack.dev/* ]] && continue
  lic=$(ls "$dir" | grep -iE '^(LICEN[CS]E|COPYING)' || true)
  [[ -z $lic ]] && { echo "NO LICENSE FILE: $mod"; fail=1; continue; }
  # Dual-licensed modules offering a permissive option are fine; only flag when no permissive text exists.
  text=$(cd "$dir" && cat $lic)
  grep -qiE 'Apache License|MIT License|Permission is hereby granted|Redistribution and use in source and binary|ISC License|Mozilla Public License' <<<"$text" && continue
  check "$mod" "$text"
done < <(go list -deps -f '{{with .Module}}{{.Path}} {{.Dir}}{{end}}' ./cmd/... | sort -u)

if [[ -d portal/node_modules ]]; then
  while IFS=$'\t' read -r name lic; do
    case "$lic" in
      MIT|Apache-2.0|BSD-2-Clause|BSD-3-Clause|ISC|MPL-2.0|0BSD) ;;
      *LGPL*|*EPL*) grep -qF "$name" compliance/APPROVALS.md || { echo "NEEDS APPROVAL ($lic): $name"; fail=1; } ;;
      *) echo "NOT ALLOWED OR UNKNOWN ($lic): $name"; fail=1 ;;
    esac
  done < <(cd portal && npm ls --omit=dev --all --parseable 2>/dev/null | tail -n +2 |
    node -e 'require("fs").readFileSync(0,"utf8").trim().split("\n").forEach(d=>{const p=require(d+"/package.json");console.log(p.name+"\t"+(p.license||""))})')
fi

[[ $fail == 0 ]] && echo "license-check: OK"
exit $fail
