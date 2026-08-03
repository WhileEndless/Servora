#!/usr/bin/env bash
# Settings resolution, validation and config merging for install.sh.
# Sourced by install.sh and by scripts/test-install.sh; defines functions only.

# Loopback entries are always granted access so a misconfigured CIDR list
# cannot lock the operator out of the machine it is running on.
readonly LOOPBACK_CIDRS='127.0.0.1/32,::1/128'
readonly DEFAULT_LISTEN='0.0.0.0:8443'
readonly MONITOR_UNIT='system-maintenance-monitor.service'

# --- reading an existing config -------------------------------------------

# config_get FILE KEY -> value on stdout, empty if absent or file missing.
config_get() {
  local file="$1" key="$2"
  [[ -r "$file" ]] || return 0
  awk -F= -v key="$key" '
    /^[[:space:]]*#/ { next }
    {
      k = $1
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", k)
      if (k != key) next
      sub(/^[^=]*=/, "")
      gsub(/^[[:space:]]+|[[:space:]]+$/, "")
      gsub(/^["'"'"']|["'"'"']$/, "")
      value = $0
    }
    END { if (value != "") print value }
  ' "$file"
}

# config_set FILE KEY VALUE
# Rewrites KEY in place when present, appends it otherwise. Comments, ordering
# and unrelated keys survive. The write is atomic and preserves owner and mode.
config_set() {
  local file="$1" key="$2" value="$3" tmp
  tmp="$(mktemp "${file}.XXXXXX")"

  if [[ -e "$file" ]]; then
    cat -- "$file" >"$tmp"
    chmod --reference="$file" -- "$tmp" 2>/dev/null || true
    chown --reference="$file" -- "$tmp" 2>/dev/null || true
  fi

  local merged
  merged="$(awk -v key="$key" -v value="$value" '
    BEGIN { done = 0 }
    {
      line = $0
      if (line ~ /^[[:space:]]*#/) { print line; next }
      k = line
      sub(/=.*$/, "", k)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", k)
      if (k == key && line ~ /=/) {
        if (!done) { print key "=" value; done = 1 }
        next
      }
      print line
    }
    END { if (!done) print key "=" value }
  ' "$tmp")"

  printf '%s\n' "$merged" >"$tmp"
  mv -- "$tmp" "$file"
}

# config_backup FILE -- one-time .bak snapshot before the first modification.
config_backup() {
  local file="$1"
  [[ -e "$file" && ! -e "${file}.bak" ]] || return 0
  cp -p -- "$file" "${file}.bak"
}

# --- validation ------------------------------------------------------------

valid_port() {
  local port="$1"
  [[ "$port" =~ ^[0-9]+$ ]] || return 1
  ((port >= 1 && port <= 65535))
}

# valid_listen ADDRESS -- host:port, where host may be empty, IPv4,
# bracketed IPv6, or a DNS name.
valid_listen() {
  local value="$1" host port
  [[ "$value" == *:* ]] || return 1
  port="${value##*:}"
  host="${value%:*}"
  valid_port "$port" || return 1
  [[ -z "$host" ]] && return 0
  if [[ "$host" == \[*\] ]]; then
    host="${host:1:${#host}-2}"
    [[ "$host" =~ ^[0-9a-fA-F:.]+$ ]]
    return
  fi
  [[ "$host" =~ ^[0-9A-Za-z.:_-]+$ ]]
}

valid_cidrs() {
  local value="$1" item addr prefix octet
  [[ -n "$value" ]] || return 1
  local IFS=','
  for item in $value; do
    item="${item//[[:space:]]/}"
    [[ -n "$item" ]] || return 1
    [[ "$item" == */* ]] || return 1
    addr="${item%/*}"
    prefix="${item##*/}"
    [[ "$prefix" =~ ^[0-9]+$ ]] || return 1
    if [[ "$addr" == *:* ]]; then
      [[ "$addr" =~ ^[0-9a-fA-F:]+$ ]] || return 1
      ((prefix >= 0 && prefix <= 128)) || return 1
      continue
    fi
    [[ "$addr" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || return 1
    ((prefix >= 0 && prefix <= 32)) || return 1
    local IFS='.'
    for octet in $addr; do
      ((octet >= 0 && octet <= 255)) || return 1
    done
  done
  return 0
}

valid_hosts() {
  local value="$1" item
  [[ -n "$value" ]] || return 1
  local IFS=','
  for item in $value; do
    item="${item//[[:space:]]/}"
    [[ -n "$item" ]] || return 1
    [[ "$item" =~ ^[0-9a-fA-F:.]+$ || "$item" =~ ^[A-Za-z0-9.-]+$ ]] || return 1
  done
  return 0
}

# --- autodetection ---------------------------------------------------------

# ipv4_network ADDRESS PREFIX -> network base address.
ipv4_network() {
  local addr="$1" prefix="$2" a b c d packed mask
  IFS='.' read -r a b c d <<<"$addr"
  packed=$(((a << 24) | (b << 16) | (c << 8) | d))
  if ((prefix == 0)); then
    mask=0
  else
    mask=$((0xFFFFFFFF << (32 - prefix) & 0xFFFFFFFF))
  fi
  packed=$((packed & mask))
  printf '%d.%d.%d.%d/%d\n' \
    $(((packed >> 24) & 255)) $(((packed >> 16) & 255)) \
    $(((packed >> 8) & 255)) $((packed & 255)) "$prefix"
}

# detect_cidrs -- loopback plus the networks of global IPv4 interfaces.
# Container and virtual bridges are skipped: they carry no operator traffic and
# would widen access for every container on the host.
detect_cidrs() {
  local out="$LOOPBACK_CIDRS" iface cidr network
  command -v ip >/dev/null 2>&1 || { printf '%s\n' "$out"; return 0; }
  while read -r iface cidr; do
    case "$iface" in
      docker*|veth*|br-*|virbr*|tun*|tap*|cni*|flannel*|lo) continue ;;
    esac
    [[ "$cidr" == */* ]] || continue
    network="$(ipv4_network "${cidr%/*}" "${cidr##*/}")"
    [[ ",$out," == *",$network,"* ]] || out+=",${network}"
  done < <(ip -4 -o addr show scope global 2>/dev/null | awk '{print $2, $4}')
  printf '%s\n' "$out"
}

# detect_hosts -- hostname plus this machine's routable addresses.
detect_hosts() {
  local out address
  out="$(hostname -f 2>/dev/null || hostname 2>/dev/null || echo localhost)"
  while read -r address; do
    [[ -n "$address" ]] || continue
    [[ ",$out," == *",$address,"* ]] || out+=",${address}"
  done < <(hostname -I 2>/dev/null | tr ' ' '\n')
  printf '%s\n' "$out"
}

# cert_hosts CERT_PATH -- SANs of an existing certificate, so a reinstall
# defaults to the names the deployed certificate already covers.
cert_hosts() {
  local cert="$1"
  [[ -r "$cert" ]] || return 0
  command -v openssl >/dev/null 2>&1 || return 0
  openssl x509 -in "$cert" -noout -ext subjectAltName 2>/dev/null |
    tr ',' '\n' |
    sed -n 's/.*\(DNS\|IP Address\):\([^,]*\)/\2/p' |
    tr -d ' ' |
    paste -sd, -
}

# --- port ownership --------------------------------------------------------

# port_listening PORT -- true when any socket listens on PORT. Ownership needs
# root, but the presence of the socket does not, so the two are kept separate:
# an unidentifiable owner still means the port is taken.
port_listening() {
  local port="$1"
  command -v ss >/dev/null 2>&1 || return 1
  [[ -n "$(ss -lntnH "sport = :${port}" 2>/dev/null)" ]]
}

# port_pid PORT -> pid of the listener, empty when free or not visible to us.
port_pid() {
  local port="$1"
  command -v ss >/dev/null 2>&1 || return 0
  ss -lntnpH "sport = :${port}" 2>/dev/null |
    sed -n 's/.*pid=\([0-9]\+\).*/\1/p' |
    head -n 1
}

# port_process PID -> human-readable process name.
port_process() {
  local pid="$1"
  [[ -n "$pid" && -r "/proc/${pid}/comm" ]] || { echo "unknown"; return 0; }
  printf '%s (pid %s)\n' "$(<"/proc/${pid}/comm")" "$pid"
}

# pid_is_monitor PID -- true when the listener is our own monitor unit, which
# legitimately holds the port on every upgrade and is not a conflict.
pid_is_monitor() {
  local pid="$1"
  [[ -n "$pid" && -r "/proc/${pid}/cgroup" ]] || return 1
  grep -q -- "$MONITOR_UNIT" "/proc/${pid}/cgroup"
}

# port_available PORT -- free, or held by our own monitor. An occupied port
# whose owner we cannot identify counts as unavailable: the socket is real
# whether or not we are allowed to see who opened it.
port_available() {
  local port="$1" pid
  port_listening "$port" || return 0
  pid="$(port_pid "$port")"
  [[ -n "$pid" ]] && pid_is_monitor "$pid"
}

# next_free_port PORT -- first available port at or above PORT.
next_free_port() {
  local port="$1" limit=$(($1 + 200))
  while ((port <= limit && port <= 65535)); do
    if port_available "$port"; then
      printf '%s\n' "$port"
      return 0
    fi
    ((port++))
  done
  return 1
}

# --- resolution ------------------------------------------------------------

# resolve SETTING ENV_VALUE CONFIG_VALUE DETECTED_VALUE DEFAULT_VALUE
# Applies the documented precedence and validates whichever source wins. An
# invalid environment value is fatal rather than silently falling through.
# validate SETTING VALUE -- dispatches to the setting's validator. Callers must
# not build the function name inline: a single `local a=$1 b=valid_$a` expands
# b before a is assigned, which silently produces a validator that rejects
# everything.
validate() {
  local validator="valid_$1"
  declare -F "$validator" >/dev/null || return 2
  "$validator" "$2"
}

resolve() {
  local setting="$1" env_value="$2" config_value="$3" detected="$4" fallback="$5"

  if [[ -n "$env_value" ]]; then
    if ! validate "$setting" "$env_value"; then
      echo "Invalid value for ${setting}: ${env_value}" >&2
      return 2
    fi
    printf '%s\n' "$env_value"
    return 0
  fi
  local candidate
  for candidate in "$config_value" "$detected" "$fallback"; do
    if [[ -n "$candidate" ]] && validate "$setting" "$candidate"; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done
  return 1
}

# ask SETTING PROMPT DEFAULT -> accepted value on stdout.
# Re-asks until the answer validates. Bounded, so a validator that can never be
# satisfied — or a closed terminal returning EOF forever — fails the install
# instead of spinning.
ask() {
  local setting="$1" prompt="$2" default="$3" answer attempt
  for ((attempt = 0; attempt < 10; attempt++)); do
    if ! read -r -p "${prompt} [${default}]: " answer </dev/tty; then
      answer=''
      attempt=10
    fi
    answer="${answer:-$default}"
    if validate "$setting" "$answer"; then
      printf '%s\n' "$answer"
      return 0
    fi
    echo "  Geçersiz değer: ${answer}" >&2
  done
  echo "Geçerli bir ${setting} değeri alınamadı." >&2
  return 1
}

# A terminal on stdin is the signal to prompt. Piped installs (curl | sudo bash)
# and CI runs fall through to the resolution chain untouched.
interactive() { [[ "${NONINTERACTIVE:-false}" != "true" && -t 0 ]]; }
