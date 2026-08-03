#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "Kurulum root yetkisi ister: sudo ./install.sh" >&2
  exit 1
fi

source_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
if [[ ! -x "$source_dir/build/system-maintenance-monitor" || ! -x "$source_dir/build/system-maintenance-agent" ]]; then
  echo "Derlenmiş binary bulunamadı. Önce 'make build' çalıştırın." >&2
  exit 1
fi

# shellcheck source=scripts/lib/install-lib.sh
source "$source_dir/scripts/lib/install-lib.sh"

monitor_conf=/etc/system-maintenance/monitor.conf
tls_cert=/etc/system-maintenance/tls/server.crt
tls_key=/etc/system-maintenance/tls/server.key

# --- Ayarlar ---------------------------------------------------------------
# Sıra: ortam değişkeni > mevcut kurulum > otomatik tespit > sabit varsayılan.
# Sorular sistemde hiçbir değişiklik yapılmadan önce sorulur.

listen="$(resolve listen "${LISTEN:-}" "$(config_get "$monitor_conf" LISTEN)" '' "$DEFAULT_LISTEN")"
allowed_cidrs="$(resolve cidrs "${ALLOWED_CIDRS:-}" "$(config_get "$monitor_conf" ALLOWED_CIDRS)" "$(detect_cidrs)" "$LOOPBACK_CIDRS")"
hosts="$(resolve hosts "${HOSTS:-}" "$(cert_hosts "$tls_cert")" "$(detect_hosts)" "$(hostname)")"

if interactive; then
  echo "Kurulum ayarları (Enter = köşeli parantez içindeki değer):"
  [[ -n "${LISTEN:-}" ]] || listen="$(ask listen "  Dinlenecek adres:port" "$listen")"
  [[ -n "${ALLOWED_CIDRS:-}" ]] || allowed_cidrs="$(ask cidrs "  Erişime izinli ağlar" "$allowed_cidrs")"
  if [[ ! -e "$tls_cert" ]]; then
    [[ -n "${HOSTS:-}" ]] || hosts="$(ask hosts "  Sertifika adresleri" "$hosts")"
  fi
  echo
fi

# Port çakışması: portu kendi monitor servisimiz tutuyorsa bu bir çakışma
# değildir, her yükseltmede öyle görünür.
listen_port="${listen##*:}"
if ! port_available "$listen_port"; then
  holder_pid="$(port_pid "$listen_port")"
  if [[ -n "$holder_pid" ]]; then
    holder="$(port_process "$holder_pid")"
  else
    holder="tanımlanamayan bir süreç"
  fi
  if interactive && suggestion="$(next_free_port "$((listen_port + 1))")"; then
    echo "${listen_port} portu ${holder} tarafından kullanılıyor." >&2
    read -r -p "  ${suggestion} portu kullanılsın mı? [E/h]: " reply </dev/tty || reply=''
    case "${reply:-e}" in
      [Ee]*|'') listen="${listen%:*}:${suggestion}"; listen_port="$suggestion" ;;
      *) echo "Kurulum durduruldu. Portu boşaltın veya LISTEN=adres:port ile farklı bir port verin." >&2; exit 1 ;;
    esac
  else
    echo "${listen_port} portu ${holder} tarafından kullanılıyor." >&2
    echo "Portu boşaltın veya farklı bir port verin: make install LISTEN=${listen%:*}:9443" >&2
    exit 1
  fi
fi

getent group system-maintenance-agent >/dev/null ||
  groupadd --system system-maintenance-agent
getent group system-maintenance-admin >/dev/null ||
  groupadd --system system-maintenance-admin
if ! id system-maintenance >/dev/null 2>&1; then
  useradd --system --gid system-maintenance-agent --home-dir /var/lib/system-maintenance-monitor \
    --shell /usr/sbin/nologin system-maintenance
fi

if [[ -n "${SUDO_USER:-}" && "${SUDO_USER}" != "root" ]]; then
  usermod -a -G system-maintenance-admin "$SUDO_USER"
  echo "Authorized ${SUDO_USER} for web login through system-maintenance-admin."
else
  echo "Authorize an existing Linux user with:"
  echo "  make admin-add ADMIN_USER=username"
fi

install -d -m 0755 /opt/system-maintenance/bin
install -d -m 0755 /opt/system-maintenance/lib
install -m 0755 "$source_dir/bin/system-maintenance" /opt/system-maintenance/bin/
install -m 0755 "$source_dir/bin/docker-image-cleaner" /opt/system-maintenance/bin/
install -m 0755 "$source_dir/bin/generate-certificate" /opt/system-maintenance/bin/
install -m 0755 "$source_dir/bin/manage-admin" /opt/system-maintenance/bin/
install -m 0755 "$source_dir/build/system-maintenance-monitor" /opt/system-maintenance/bin/
install -m 0750 "$source_dir/build/system-maintenance-agent" /opt/system-maintenance/bin/
install -m 0644 "$source_dir/build/network_accounting.bpf.o" /opt/system-maintenance/lib/
install -m 0644 "$source_dir/README.md" /opt/system-maintenance/

if [[ ! -e /etc/system-maintenance.conf ]]; then
  install -m 0644 "$source_dir/config/system-maintenance.conf" /etc/system-maintenance.conf
else
  echo "/etc/system-maintenance.conf mevcut; üzerine yazılmadı."
fi

install -d -m 0750 -o root -g system-maintenance-agent /etc/system-maintenance
install -d -m 0750 -o root -g system-maintenance-agent /etc/system-maintenance/tls
if [[ ! -e "$monitor_conf" ]]; then
  install -m 0640 -o root -g system-maintenance-agent \
    "$source_dir/config/monitor.conf" "$monitor_conf"
else
  config_backup "$monitor_conf"
fi
# Yalnızca bu iki anahtar güncellenir; yorumlar ve elle yapılan diğer
# düzenlemeler olduğu gibi kalır.
config_set "$monitor_conf" LISTEN "$listen"
config_set "$monitor_conf" ALLOWED_CIDRS "$allowed_cidrs"
install -d -m 0750 -o system-maintenance -g system-maintenance-agent \
  /var/lib/system-maintenance-monitor
install -d -m 2750 -o root -g system-maintenance-agent \
  /var/lib/system-maintenance-monitor/secrets

if [[ -e /etc/pam.d/common-auth ]]; then
  install -m 0644 "$source_dir/pam/system-maintenance.debian" /etc/pam.d/system-maintenance
elif [[ -e /etc/pam.d/system-auth ]]; then
  install -m 0644 "$source_dir/pam/system-maintenance.rhel" /etc/pam.d/system-maintenance
else
  echo "Desteklenen PAM temel profili bulunamadı; /etc/pam.d/system-maintenance oluşturulamadı." >&2
  exit 1
fi

install -m 0644 "$source_dir/systemd/system-maintenance.service" /etc/systemd/system/
install -m 0644 "$source_dir/systemd/system-maintenance.timer" /etc/systemd/system/
install -m 0644 "$source_dir/systemd/system-maintenance-agent.service" /etc/systemd/system/
install -m 0644 "$source_dir/systemd/system-maintenance-monitor.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now system-maintenance.timer

# Sertifika yoksa üretilir; böylece 'make install' tek başına çalışan bir
# sistem bırakır. Mevcut sertifikaya dokunulmaz.
if [[ ! -e "$tls_cert" && ! -e "$tls_key" ]]; then
  /opt/system-maintenance/bin/generate-certificate "$tls_cert" "$tls_key" "$hosts"
  chown root:system-maintenance-agent "$tls_key"
fi

# try-restart: çalışmayan servisleri başlatmaz, çalışanlara yeni ayarı uygular.
systemctl try-restart system-maintenance-agent.service system-maintenance-monitor.service

echo
echo "Kurulum tamamlandı."
echo "  Dinlenen adres : ${listen}"
echo "  İzinli ağlar   : ${allowed_cidrs}"
echo "Servisleri başlatmak için 'make start' kullanın."
echo "Tüm komutları görmek için: make help"
