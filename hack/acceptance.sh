#!/usr/bin/env bash
# Copyright 2026 The KubeStack Authors. All rights reserved.
#
# Acceptance test on real infrastructure (docs/acceptance.md): drives the KubeStack API through the full
# lifecycle of one RKE2 cluster on VMs you provide. Destructive for that cluster only.
# Needs: curl, jq. Env: KUBESTACK_URL, KUBESTACK_TOKEN (an OIDC ID token of an admin-group member).
# Usage: hack/acceptance.sh <cluster name> <from version> <to version>
set -euo pipefail
NAME=${1:?usage: hack/acceptance.sh <name> <from version> <to version>}
FROM=${2:?} TO=${3:?}
: "${KUBESTACK_URL:?}" "${KUBESTACK_TOKEN:?}"

api() { # method path [body] [idempotent]
  local args=(-sS -X "$1" -H "Authorization: Bearer $KUBESTACK_TOKEN" -H 'Content-Type: application/json' -w '\n%{http_code}')
  [[ -n ${3:-} ]] && args+=(-d "$3")
  [[ -n ${4:-} ]] && args+=(-H "Idempotency-Key: acceptance-$NAME-$4")
  local out; out=$(curl "${args[@]}" "$KUBESTACK_URL/api/v1$2")
  CODE=${out##*$'\n'} BODY=${out%$'\n'*}
}
expect() { [[ $CODE == "$1" ]] || { echo "FAIL: $2 (HTTP $CODE): $BODY"; exit 1; }; echo "ok: $2"; }
wait_for() { # description, jq filter on GET path, path, minutes
  for _ in $(seq $(( $4 * 6 ))); do api GET "$3"; [[ $CODE == 200 ]] && jq -e "$2" <<<"$BODY" >/dev/null && { echo "ok: $1"; return; }; sleep 10; done
  echo "FAIL: timed out: $1"; exit 1
}

api POST /clusters "{\"name\":\"$NAME\",\"kubernetesVersion\":\"$FROM\"}"; expect 201 "create $NAME ($FROM)"
wait_for "registration command issued" '.command | contains("{token}")' "/clusters/$NAME/registration" 5
api GET "/clusters/$NAME/registration"
echo; echo ">>> Join 3 control-plane+etcd+worker nodes and 1 worker using the portal's 'Add nodes' script, then press Enter."; read -r
wait_for "4 nodes ready" 'length == 4 and all(.ready)' "/clusters/$NAME/nodes" 30
wait_for "cluster ready" '.ready' "/clusters/$NAME" 15

api POST "/clusters/$NAME/snapshots"; expect 202 "take snapshot"
wait_for "snapshot successful" 'any(.successful)' "/clusters/$NAME/snapshots" 15
SNAP=$(jq -r '[.[] | select(.successful)] | last | .name' <<<"$BODY")

api POST "/clusters/$NAME/restore" "{\"snapshot\":\"$SNAP\"}" restore1; expect 202 "restore $SNAP"
wait_for "cluster ready after restore" '.ready' "/clusters/$NAME" 30

api POST "/clusters/$NAME/upgrade" "{\"kubernetesVersion\":\"$TO\"}" up1; expect 202 "upgrade to $TO"
api POST "/clusters/$NAME/upgrade" "{\"kubernetesVersion\":\"$TO\"}" up1; expect 202 "replay with the same Idempotency-Key"
wait_for "running $TO and ready" ".kubernetesVersion == \"$TO\" and .ready" "/clusters/$NAME" 60

api GET "/clusters/$NAME/nodes"
CP=$(jq -r '[.[] | select(.roles | index("etcd"))] | first | .name' <<<"$BODY")
WORKER=$(jq -r '[.[] | select(.roles == ["worker"])] | first | .name' <<<"$BODY")
api DELETE "/clusters/$NAME/nodes/$WORKER" "" rm1; expect 202 "remove worker $WORKER"
wait_for "3 nodes left" 'length == 3' "/clusters/$NAME/nodes" 15

api GET "/clusters/$NAME/access"; expect 200 "direct-access info"
echo ">>> Portal 'Connect' kubeconfig: run 'kubectl get nodes' as an admin-group user (expect 3 nodes)"
echo "    and as a non-admin user (expect Forbidden). Press Enter when both behave."; read -r

api DELETE "/clusters/$NAME" "" del1; expect 202 "delete $NAME"
for _ in $(seq 90); do api GET "/clusters/$NAME"; [[ $CODE == 404 ]] && break; sleep 10; done
expect 404 "cluster deleted"
api GET "/audit?limit=50"
for a in create snapshot restore upgrade remove-node delete; do
  jq -e --arg a "$a" --arg n "$NAME" 'any(.action == $a and .clusterId == $n and .result == "success")' <<<"$BODY" >/dev/null || { echo "FAIL: no successful audit record for $a"; exit 1; }
done
echo "ok: audit trail has every operation"
echo "PASS: acceptance for $NAME ($FROM -> $TO); an etcd node remains: $CP"
