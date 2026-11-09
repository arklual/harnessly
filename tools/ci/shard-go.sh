#!/bin/sh
# Prints the Go packages of shard $1 of $2 (1-based): the heaviest first, each to the lightest shard so far, by the
# weights in go-test-weights.txt (a package without one weighs 3). Plain round-robin left one job a minute longer
# than the others. With a third argument, only the packages that file lists (affected-go.sh; `./...` is all of them).
set -eu
index=${1:?shard index}
total=${2:?shard count}
only=${3:-}
dir=$(dirname "$0")
packages() { if [ -n "$only" ] && [ "$(cat "$only")" != ./... ]; then cat "$only"; else go list ./...; fi; }
packages | awk -v n="$total" -v i="$index" -v wf="$dir/go-test-weights.txt" '
  BEGIN { while ((getline line < wf) > 0) { if (line ~ /^#/ || line == "") continue; split(line, f, " "); w[f[1]] = f[2] } }
  { pkgs[NR] = $0; p = $0; sub(/^github.com\/ly-solutions\/harnessly\/?/, "", p); wt[NR] = (p in w) ? w[p] : 3; idx[NR] = NR }
  END {
    # selection order: heaviest first (stable by position)
    for (a = 1; a <= NR; a++) order[a] = a
    for (a = 2; a <= NR; a++) { v = order[a]; b = a - 1; while (b >= 1 && wt[order[b]] < wt[v]) { order[b + 1] = order[b]; b-- } order[b + 1] = v }
    for (s = 1; s <= n; s++) load[s] = 0
    for (a = 1; a <= NR; a++) {
      k = order[a]; best = 1
      for (s = 2; s <= n; s++) if (load[s] < load[best]) best = s
      load[best] += wt[k]
      if (best == i) print pkgs[k]
    }
  }'
