#!/usr/bin/env bash
# Exercise the record filter API against a Dublyobase instance.
#
#   ./filter-test.sh <base-url> <email> <password> <project> <collection>
#
# Each case prints the filter and how many rows matched, so you can eyeball
# whether the numbers are consistent (e.g. a filter plus its negation should
# add up to the unfiltered total).
set -euo pipefail

BASE=${1:?base url}          # https://your-instance
EMAIL=${2:?admin email}
PASS=${3:?admin password}
PROJECT=${4:?project slug}
COLL=${5:?collection name}

TOKEN=$(curl -s -X POST "$BASE/admin/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}" \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["token"])')

R="$BASE/api/projects/$PROJECT/collections/$COLL/records"

run() {  # run <label> <filter-json>
  local out code
  out=$(curl -s -G "$R" \
        --data-urlencode "filter=$2" --data-urlencode 'perPage=1' \
        -H "Authorization: Bearer $TOKEN" -w '\n%{http_code}')
  code=$(tail -1 <<<"$out")
  body=$(sed '$d' <<<"$out")
  printf '%-46s %s  %s\n' "$1" "$code" \
    "$(python3 -c '
import sys,json
d=json.load(sys.stdin)
print(d.get("totalItems", d.get("message","")[:60]))' <<<"$body")"
}

echo "== baseline =="
run "no filter"                     '{}'

echo; echo "== equality and comparison =="
run "_eq"                           '{"'"${FIELD:-role}"'":{"_eq":"user"}}'
run "_neq"                          '{"'"${FIELD:-role}"'":{"_neq":"user"}}'
run "_in"                           '{"'"${FIELD:-role}"'":{"_in":["user","assistant"]}}'
run "_gt on a number"               '{"'"${NUM:-output_tokens}"'":{"_gt":500}}'
run "_lte on a number"              '{"'"${NUM:-output_tokens}"'":{"_lte":500}}'

echo; echo "== text matching =="
run "_icontains"                    '{"'"${TEXT:-content}"'":{"_icontains":"answer"}}'
run "_istarts_with"                 '{"'"${TEXT:-content}"'":{"_istarts_with":"<p>"}}'

echo; echo "== null and empty =="
run "_null true"                    '{"'"${REL:-parent}"'":{"_null":true}}'
run "_nnull"                        '{"'"${REL:-parent}"'":{"_nnull":true}}'

echo; echo "== boolean logic =="
run "_or"                           '{"_or":[{"'"${FIELD:-role}"'":{"_eq":"user"}},{"'"${FIELD:-role}"'":{"_eq":"tool"}}]}'
run "_and (implicit)"               '{"'"${FIELD:-role}"'":{"_eq":"assistant"},"'"${NUM:-output_tokens}"'":{"_gt":500}}'
run "_not"                          '{"_not":{"'"${FIELD:-role}"'":{"_eq":"user"}}}'

echo; echo "== through relations =="
run "1 hop"                         '{"'"${HOP1:-conversation.archived}"'":{"_eq":true}}'
run "2 hops"                        '{"'"${HOP2:-conversation.workspace.plan}"'":{"_eq":"enterprise"}}'
run "relation + local"              '{"'"${HOP2:-conversation.workspace.plan}"'":{"_eq":"enterprise"},"'"${FIELD:-role}"'":{"_eq":"user"}}'

echo; echo "== these must be rejected, not 500 =="
run "unknown field"                 '{"definitely_not_a_field":{"_eq":1}}'
run "unknown relation leaf"         '{"'"${REL:-conversation}"'.nope":{"_eq":1}}'
run "walking a non-relation"        '{"'"${FIELD:-role}"'.name":{"_eq":"x"}}'
run "empty string into a uuid"      '{"'"${REL:-conversation}"'":{"_eq":""}}'
run "path too deep"                 '{"a.b.c.d.e.f":{"_eq":1}}'
run "malformed json"                '{"role":'
