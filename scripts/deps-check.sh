#!/usr/bin/env bash
# Verifies every build prerequisite before the build starts, so a missing
# development header surfaces here by name instead of as a C compiler error in
# the middle of a Go build. Nothing is installed automatically.
set -Eeuo pipefail

missing_labels=()
missing_apt=()
missing_dnf=()
missing_pacman=()

record() {
  missing_labels+=("$1")
  missing_apt+=("$2")
  missing_dnf+=("$3")
  missing_pacman+=("$4")
}

need_command() {
  local binary="$1"
  command -v "$binary" >/dev/null 2>&1 && return 0
  record "$binary" "$2" "$3" "$4"
}

c_compiler() {
  local candidate
  for candidate in "${CC:-}" cc gcc clang; do
    [[ -n "$candidate" ]] && command -v "$candidate" >/dev/null 2>&1 && {
      printf '%s\n' "$candidate"
      return 0
    }
  done
  return 1
}

# Probe with the preprocessor when a compiler exists, since it honours the real
# include path. Fall back to the conventional location otherwise.
need_header() {
  local header="$1" compiler
  if compiler="$(c_compiler)"; then
    printf '#include <%s>\n' "$header" | "$compiler" -E -x c - >/dev/null 2>&1 && return 0
  elif [[ -e "/usr/include/${header}" ]]; then
    return 0
  fi
  record "$header" "$2" "$3" "$4"
}

need_command go golang-go golang go
need_command node nodejs nodejs nodejs
need_command npm npm npm npm
need_command clang clang clang clang
need_command bpftool bpftool bpftool bpf
need_command llvm-strip llvm llvm llvm

if ! c_compiler >/dev/null; then
  record "C compiler (cc)" build-essential gcc base-devel
fi

need_header security/pam_appl.h libpam0g-dev pam-devel pam
need_header bpf/bpf_helpers.h libbpf-dev libbpf-devel libbpf

if ((${#missing_labels[@]} == 0)); then
  echo "All build dependencies present."
  exit 0
fi

echo "Missing build dependencies:" >&2
for label in "${missing_labels[@]}"; do
  echo "  - ${label}" >&2
done
echo >&2

declare -a packages=()
manager=""
if command -v apt-get >/dev/null 2>&1; then
  manager="sudo apt-get install -y"
  packages=("${missing_apt[@]}")
elif command -v dnf >/dev/null 2>&1; then
  manager="sudo dnf install -y"
  packages=("${missing_dnf[@]}")
elif command -v pacman >/dev/null 2>&1; then
  manager="sudo pacman -S --needed"
  packages=("${missing_pacman[@]}")
fi

if [[ -n "$manager" ]]; then
  mapfile -t packages < <(printf '%s\n' "${packages[@]}" | sort -u)
  echo "Install them with:" >&2
  echo "  ${manager} ${packages[*]}" >&2
else
  echo "Install the packages providing the items above for your distribution." >&2
fi
echo >&2
echo "Then re-check with: make deps-check" >&2
exit 2
