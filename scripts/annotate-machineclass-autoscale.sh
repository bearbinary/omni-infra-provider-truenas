#!/usr/bin/env bash
# annotate-machineclass-autoscale.sh — Opt an Omni MachineClass into the
# experimental TrueNAS autoscaler by setting the required annotations.
#
# Annotations set (see docs/autoscaler.md):
#   bearbinary.com/autoscale-min
#   bearbinary.com/autoscale-max
#   bearbinary.com/autoscale-pool                (optional; falls back to DEFAULT_POOL)
#   bearbinary.com/autoscale-capacity-gate       (optional; hard|soft, default hard)
#   bearbinary.com/autoscale-min-pool-free-gib   (optional; default 50, 0 disables)
#   bearbinary.com/autoscale-min-host-mem-gib    (forced to 0 — mem gate not wired yet)
#
# Prerequisites:
#   - omnictl authenticated against the Omni instance that owns the MachineClass
#   - yq v4+ (https://mikefarah.gitbook.io/yq) OR python3 (auto-detected)
#
# Usage:
#   ./scripts/annotate-machineclass-autoscale.sh <class-name> <min> <max> [flags]
#
# Flags:
#   --pool <name>              TrueNAS pool for the capacity gate
#   --capacity-gate <hard|soft>
#   --min-pool-free-gib <int>  Pool-free threshold; 0 disables
#   --min-host-mem-gib <int>   Host-mem threshold (default 0 until gate lands)
#   --dry-run                  Print the patched YAML, do not apply
#
# Examples:
#   ./scripts/annotate-machineclass-autoscale.sh talos-home-workers 2 8
#   ./scripts/annotate-machineclass-autoscale.sh workers 2 10 --pool tank --capacity-gate soft

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

info() { echo -e "${YELLOW}[INFO]${NC} $1"; }
pass() { echo -e "${GREEN}[DONE]${NC} $1"; }
fail() { echo -e "${RED}[FAIL]${NC} $1" >&2; exit 1; }

# ─── Args ───────────────────────────────────────────────────────────────────

if [[ $# -lt 3 ]]; then
  sed -n '2,30p' "$0"
  exit 1
fi

CLASS_NAME="$1"
MIN="$2"
MAX="$3"
shift 3

POOL=""
CAPACITY_GATE=""
MIN_POOL_FREE_GIB=""
MIN_HOST_MEM_GIB="0"
DRY_RUN="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pool)              POOL="$2"; shift 2 ;;
    --capacity-gate)     CAPACITY_GATE="$2"; shift 2 ;;
    --min-pool-free-gib) MIN_POOL_FREE_GIB="$2"; shift 2 ;;
    --min-host-mem-gib)  MIN_HOST_MEM_GIB="$2"; shift 2 ;;
    --dry-run)           DRY_RUN="true"; shift ;;
    *) fail "unknown flag: $1" ;;
  esac
done

# ─── Validate ───────────────────────────────────────────────────────────────

[[ "$MIN" =~ ^[0-9]+$ ]] || fail "min must be a non-negative integer, got: $MIN"
[[ "$MAX" =~ ^[0-9]+$ ]] || fail "max must be a non-negative integer, got: $MAX"
(( MIN <= MAX )) || fail "min ($MIN) must be <= max ($MAX)"
(( MIN >= 1 ))   || fail "min must be >= 1 (scale-from-zero not yet supported; see docs/autoscaler.md)"

if [[ -n "$CAPACITY_GATE" && "$CAPACITY_GATE" != "hard" && "$CAPACITY_GATE" != "soft" ]]; then
  fail "--capacity-gate must be 'hard' or 'soft', got: $CAPACITY_GATE"
fi
if [[ -n "$MIN_POOL_FREE_GIB" && ! "$MIN_POOL_FREE_GIB" =~ ^[0-9]+$ ]]; then
  fail "--min-pool-free-gib must be a non-negative integer, got: $MIN_POOL_FREE_GIB"
fi
[[ "$MIN_HOST_MEM_GIB" =~ ^[0-9]+$ ]] || fail "--min-host-mem-gib must be a non-negative integer"

if [[ "$MIN_HOST_MEM_GIB" != "0" ]]; then
  info "WARNING: --min-host-mem-gib=$MIN_HOST_MEM_GIB — the mem gate is NOT wired yet (see docs/autoscaler.md). Set to 0 unless you know what you are doing."
fi

# ─── Tooling ────────────────────────────────────────────────────────────────

command -v omnictl >/dev/null 2>&1 || fail "omnictl not found in PATH"

PATCHER=""
if command -v yq >/dev/null 2>&1; then
  PATCHER="yq"
elif command -v python3 >/dev/null 2>&1; then
  PATCHER="python3"
else
  fail "need either 'yq' or 'python3' to patch the MachineClass YAML"
fi

# ─── Fetch ──────────────────────────────────────────────────────────────────

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
ORIG="$TMP_DIR/mc.yaml"
PATCHED="$TMP_DIR/mc.patched.yaml"

info "fetching MachineClass '$CLASS_NAME' from Omni"
omnictl get machineclasses "$CLASS_NAME" -o yaml > "$ORIG" \
  || fail "failed to fetch MachineClass '$CLASS_NAME' — is omnictl configured for the right Omni instance?"

# ─── Patch ──────────────────────────────────────────────────────────────────

# Build list of annotations to set (skip unset optionals so we don't overwrite
# existing keys with empty strings).
declare -a KEYS VALS
KEYS+=("bearbinary.com/autoscale-min");            VALS+=("$MIN")
KEYS+=("bearbinary.com/autoscale-max");            VALS+=("$MAX")
KEYS+=("bearbinary.com/autoscale-min-host-mem-gib"); VALS+=("$MIN_HOST_MEM_GIB")
if [[ -n "$POOL" ]]; then
  KEYS+=("bearbinary.com/autoscale-pool");         VALS+=("$POOL")
fi
if [[ -n "$CAPACITY_GATE" ]]; then
  KEYS+=("bearbinary.com/autoscale-capacity-gate"); VALS+=("$CAPACITY_GATE")
fi
if [[ -n "$MIN_POOL_FREE_GIB" ]]; then
  KEYS+=("bearbinary.com/autoscale-min-pool-free-gib"); VALS+=("$MIN_POOL_FREE_GIB")
fi

info "patching annotations ($PATCHER)"
if [[ "$PATCHER" == "yq" ]]; then
  cp "$ORIG" "$PATCHED"
  for i in "${!KEYS[@]}"; do
    k="${KEYS[$i]}" v="${VALS[$i]}" yq -i '.metadata.annotations[strenv(k)] = strenv(v)' "$PATCHED"
  done
else
  # python3 fallback. PyYAML is stdlib on most distros via `python3 -c`.
  python3 - "$ORIG" "$PATCHED" "${KEYS[@]}" "--sep--" "${VALS[@]}" <<'PY'
import sys, yaml
orig, patched = sys.argv[1], sys.argv[2]
rest = sys.argv[3:]
sep = rest.index("--sep--")
keys, vals = rest[:sep], rest[sep+1:]
assert len(keys) == len(vals), "keys/vals length mismatch"
with open(orig) as f:
    doc = yaml.safe_load(f)
meta = doc.setdefault("metadata", {})
ann  = meta.setdefault("annotations", {}) or {}
for k, v in zip(keys, vals):
    ann[k] = v
meta["annotations"] = ann
with open(patched, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PY
fi

echo
echo "── proposed annotations ───────────────────────────────────"
for i in "${!KEYS[@]}"; do
  printf "  %s: %s\n" "${KEYS[$i]}" "${VALS[$i]}"
done
echo "───────────────────────────────────────────────────────────"
echo

if [[ "$DRY_RUN" == "true" ]]; then
  info "--dry-run set; patched YAML:"
  cat "$PATCHED"
  exit 0
fi

# ─── Apply ──────────────────────────────────────────────────────────────────

info "applying patched MachineClass via omnictl"
omnictl apply -f "$PATCHED" || fail "omnictl apply failed"

# ─── Verify ─────────────────────────────────────────────────────────────────

info "verifying annotations on Omni"
VERIFY="$(omnictl get machineclasses "$CLASS_NAME" -o yaml)"
missing=0
for i in "${!KEYS[@]}"; do
  k="${KEYS[$i]}" v="${VALS[$i]}"
  if ! grep -Fq "$k: \"$v\"" <<<"$VERIFY" && ! grep -Fq "$k: $v" <<<"$VERIFY"; then
    echo "  [MISS] $k=$v"
    missing=1
  fi
done
(( missing == 0 )) || fail "one or more annotations did not round-trip; inspect with: omnictl get machineclasses $CLASS_NAME -o yaml"

pass "MachineClass '$CLASS_NAME' opted into autoscaling (min=$MIN, max=$MAX)"
echo
info "next: deploy the autoscaler Helm chart — see docs/autoscaler.md#deploy"
