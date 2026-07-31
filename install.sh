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
if [[ ! -e /etc/system-maintenance/monitor.conf ]]; then
  install -m 0640 -o root -g system-maintenance-agent \
    "$source_dir/config/monitor.conf" /etc/system-maintenance/monitor.conf
else
  echo "/etc/system-maintenance/monitor.conf mevcut; üzerine yazılmadı."
fi
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
echo "Dosyalar kuruldu. HTTPS sertifikası için 'make cert-generate HOSTS=\"IP,DNS\"' çalıştırın."
echo "Ardından servisleri başlatmak için 'make start' kullanın."
echo "Tüm komutları görmek için: make help"
