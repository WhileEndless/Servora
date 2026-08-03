#!/usr/bin/env bash
# Tests for the installer's settings resolution, validation and config merging.
# Runs without root and touches nothing outside a temporary directory.
set -Eeuo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/install-lib.sh
source "$root/scripts/lib/install-lib.sh"

passed=0
failed=0
workdir="$(mktemp -d)"
trap 'rm -rf -- "$workdir"' EXIT

ok() { printf '  ok    %s\n' "$1"; passed=$((passed + 1)); }
no() { printf '  FAIL  %s\n     expected: %s\n     actual:   %s\n' "$1" "$2" "$3"; failed=$((failed + 1)); }

equals() {
  local name="$1" expected="$2" actual="$3"
  [[ "$expected" == "$actual" ]] && ok "$name" || no "$name" "$expected" "$actual"
}

# fn may be a function name or a function plus leading arguments, so it is
# deliberately left unquoted for word splitting.
accepts() {
  local name="$1" fn="$2" value="$3"
  # shellcheck disable=SC2086
  if $fn "$value"; then ok "$name"; else no "$name" "accepted" "rejected"; fi
}

rejects() {
  local name="$1" fn="$2" value="$3"
  # shellcheck disable=SC2086
  if $fn "$value"; then no "$name" "rejected" "accepted"; else ok "$name"; fi
}

echo "validation"
accepts "listener with bind address" valid_listen "0.0.0.0:8443"
accepts "listener with bare port"    valid_listen ":8443"
accepts "bracketed IPv6 listener"    valid_listen "[::1]:8443"
rejects "listener without a port"    valid_listen "0.0.0.0"
rejects "port above the range"       valid_listen "0.0.0.0:70000"
rejects "port zero"                  valid_listen "0.0.0.0:0"
rejects "non-numeric port"           valid_listen "0.0.0.0:https"

accepts "single CIDR"        valid_cidrs "10.0.0.0/8"
accepts "CIDR list"          valid_cidrs "127.0.0.1/32,::1/128,10.1.0.0/16"
rejects "bare address"       valid_cidrs "10.0.0.1"
rejects "octet out of range" valid_cidrs "10.0.0.300/24"
rejects "prefix too large"   valid_cidrs "10.0.0.0/33"
rejects "empty CIDR list"    valid_cidrs ""

accepts "DNS certificate host" valid_hosts "monitor.example.lan"
accepts "mixed host list"      valid_hosts "203.0.113.10,monitor.example.lan"
rejects "host with a slash"    valid_hosts "monitor.example.lan/x"
rejects "empty host list"      valid_hosts ""

echo "network arithmetic"
equals "class C network"     "203.0.113.0/24"  "$(ipv4_network 203.0.113.42 24)"
equals "class B network"     "172.16.0.0/16"   "$(ipv4_network 172.16.31.9 16)"
equals "host route"          "10.1.2.3/32"     "$(ipv4_network 10.1.2.3 32)"
equals "unaligned prefix"    "10.1.2.0/28"     "$(ipv4_network 10.1.2.9 28)"

echo "config reading"
conf="$workdir/monitor.conf"
cat >"$conf" <<'EOF'
# HTTPS listener
LISTEN=0.0.0.0:8443
ALLOWED_CIDRS=127.0.0.1/32

# retention
MAX_DATABASE_MB=2048
EOF

equals "reads a key"            "0.0.0.0:8443" "$(config_get "$conf" LISTEN)"
equals "absent key is empty"    ""             "$(config_get "$conf" NOPE)"
equals "missing file is empty"  ""             "$(config_get "$workdir/none.conf" LISTEN)"

echo "config merging"
config_set "$conf" LISTEN "0.0.0.0:9443"
equals "updates in place"      "0.0.0.0:9443"  "$(config_get "$conf" LISTEN)"
equals "leaves other keys"     "2048"          "$(config_get "$conf" MAX_DATABASE_MB)"
equals "keeps comments"        "2"             "$(grep -c '^#' "$conf")"
equals "does not duplicate"    "1"             "$(grep -c '^LISTEN=' "$conf")"

config_set "$conf" TRUSTED_PROXIES "203.0.113.1/32"
equals "appends a new key"     "203.0.113.1/32" "$(config_get "$conf" TRUSTED_PROXIES)"

config_backup "$conf"
equals "takes one backup"      "0.0.0.0:9443"  "$(config_get "${conf}.bak" LISTEN)"
config_set "$conf" LISTEN "0.0.0.0:7443"
config_backup "$conf"
equals "does not re-backup"    "0.0.0.0:9443"  "$(config_get "${conf}.bak" LISTEN)"

echo "validator dispatch"
# Regression: building the validator name inline in the same `local` statement
# that assigns the setting expands it before the assignment, yielding `valid_`
# and rejecting every answer.
for setting in listen cidrs hosts; do
  if declare -F "valid_${setting}" >/dev/null; then
    ok "validator exists for ${setting}"
  else
    no "validator exists for ${setting}" "valid_${setting} defined" "missing"
  fi
done
accepts "dispatch accepts a good listener" "validate listen" "0.0.0.0:8443"
rejects "dispatch rejects a bad listener" "validate listen" "nonsense"
accepts "dispatch accepts good CIDRs"     "validate cidrs"  "10.0.0.0/8"
accepts "dispatch accepts good hosts"     "validate hosts"  "monitor.example.lan"
if validate nosuchsetting anything; then
  no "unknown setting is not silently valid" "rejected" "accepted"
else
  ok "unknown setting is not silently valid"
fi

echo "resolution chain"
equals "environment wins"  "0.0.0.0:1234" "$(resolve listen "0.0.0.0:1234" "0.0.0.0:2" "" "$DEFAULT_LISTEN")"
equals "config beats default" "0.0.0.0:2345" "$(resolve listen "" "0.0.0.0:2345" "" "$DEFAULT_LISTEN")"
equals "detected beats default" "10.0.0.0/8" "$(resolve cidrs "" "" "10.0.0.0/8" "$LOOPBACK_CIDRS")"
equals "falls back to default" "$DEFAULT_LISTEN" "$(resolve listen "" "" "" "$DEFAULT_LISTEN")"
equals "skips invalid config value" "$DEFAULT_LISTEN" "$(resolve listen "" "not-a-listener" "" "$DEFAULT_LISTEN")"

if resolve listen "not-a-listener" "" "" "$DEFAULT_LISTEN" 2>/dev/null; then
  no "invalid environment value aborts" "non-zero exit" "success"
else
  ok "invalid environment value aborts"
fi

echo "autodetection"
detected="$(detect_cidrs)"
case "$detected" in
  127.0.0.1/32,::1/128*) ok "detection always includes loopback" ;;
  *) no "detection always includes loopback" "loopback prefix" "$detected" ;;
esac
if valid_cidrs "$detected"; then ok "detected list validates"; else no "detected list validates" "valid" "$detected"; fi
if [[ "$detected" == *docker* ]]; then no "detection excludes docker" "no docker network" "$detected"; else ok "detection excludes docker"; fi
if valid_hosts "$(detect_hosts)"; then ok "detected hosts validate"; else no "detected hosts validate" "valid" "$(detect_hosts)"; fi

echo "port ownership"
# A port nothing listens on is available; a port our own monitor holds is too.
free_port="$(next_free_port 41000)"
if port_available "$free_port"; then ok "unused port is available"; else no "unused port is available" "available" "busy"; fi
equals "next_free_port returns a valid port" "0" "$(valid_port "$free_port" && echo 0 || echo 1)"

if command -v python3 >/dev/null 2>&1; then
  # Block on the listener's readiness line rather than sleeping, so the test is
  # deterministic on a loaded machine.
  exec 3< <(python3 -c "
import socket, time
s = socket.socket()
s.bind(('127.0.0.1', $free_port))
s.listen(1)
print('ready', flush=True)
time.sleep(30)
" 2>/dev/null)
  read -r -u 3 -t 20 ready || ready=''
  if [[ "$ready" != "ready" ]]; then
    echo "  skip  occupied-port tests (listener did not start)"
    exec 3<&-
    free_port=''
  fi
fi

if [[ -n "${free_port:-}" ]] && command -v python3 >/dev/null 2>&1; then
  listener_pid="$(port_pid "$free_port")"
  if port_listening "$free_port"; then ok "port_listening sees the socket"; else no "port_listening sees the socket" "listening" "free"; fi
  if port_available "$free_port"; then
    no "occupied port is detected" "busy" "available"
  else
    ok "occupied port is detected"
  fi
  other="$(next_free_port "$free_port")"
  if [[ "$other" != "$free_port" ]]; then ok "next_free_port skips the busy port"; else no "next_free_port skips the busy port" "a different port" "$other"; fi
  equals "port_pid finds the listener" "0" "$([[ -n "$listener_pid" ]] && echo 0 || echo 1)"
  [[ -n "$listener_pid" ]] && kill "$listener_pid" 2>/dev/null
  exec 3<&-
elif ! command -v python3 >/dev/null 2>&1; then
  echo "  skip  occupied-port tests (python3 unavailable)"
fi

if pid_is_monitor "$$"; then
  no "this shell is not the monitor unit" "false" "true"
else
  ok "this shell is not the monitor unit"
fi

echo
printf '%d passed, %d failed\n' "$passed" "$failed"
((failed == 0))
