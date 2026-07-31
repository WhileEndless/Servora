#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "Kaldırma root yetkisi ister: sudo ./uninstall.sh" >&2
  exit 1
fi

systemctl disable --now system-maintenance.timer 2>/dev/null || true
systemctl disable --now system-maintenance-monitor.service 2>/dev/null || true
systemctl disable --now system-maintenance-agent.service 2>/dev/null || true
rm -f /etc/systemd/system/system-maintenance.service
rm -f /etc/systemd/system/system-maintenance.timer
rm -f /etc/systemd/system/system-maintenance-monitor.service
rm -f /etc/systemd/system/system-maintenance-agent.service
rm -f /etc/pam.d/system-maintenance
systemctl daemon-reload

if [[ "${PURGE:-false}" == "true" ]]; then
  rm -rf -- /opt/system-maintenance
  rm -rf -- /etc/system-maintenance
  rm -rf -- /var/lib/system-maintenance-monitor
  rm -f -- /etc/system-maintenance.conf
  echo "Uygulama, yapılandırma, sertifika ve geçmiş veriler kalıcı olarak silindi."
else
  echo "Binary, yapılandırma, sertifika ve geçmiş veriler yedek amacıyla korundu."
  echo "Tamamen kaldırmak için 'make purge' kullanabilirsiniz."
fi
