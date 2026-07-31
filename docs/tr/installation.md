# Servora kurulum ve ilk giriş akışı

## 1. Gereksinimleri doğrulayın

Hedef Linux, systemd ve PAM kullanmalıdır. Docker isteğe bağlıdır. Kesin ağ
muhasebesi ayrıca kernel BTF ve eBPF derleme araçlarını gerektirir.

Debian/Ubuntu:

```bash
sudo apt-get update
sudo apt-get install -y golang-go gcc clang llvm libbpf-dev bpftool \
  libpam0g-dev libsqlite3-dev nodejs npm openssl
```

`github.com/cilium/ebpf` dahil Go bağımlılıkları `go.mod`/`go.sum` içinde
sabitlenmiştir ve derleme sırasında Go araçları tarafından indirilir.

```bash
make help
make test
make bpf
```

## 2. Kurun ve başlatın

Tarayıcıda kullanılacak tüm IP/DNS adlarını sertifikaya ekleyin:

```bash
make setup HOSTS="192.168.2.10,monitor.example.lan"
```

Bu akış Vue ve Go kodunu derler; servis kullanıcı/gruplarını, PAM/systemd
dosyalarını kurar; self-signed sertifika üretir ve servisleri etkinleştirir.
`sudo` çağıran kullanıcı otomatik yetkilendirilir.

Aşamalı kurulum:

```bash
make build
make install
make cert-generate HOSTS="192.168.2.10,monitor.example.lan"
make start
```

## 3. Linux kullanıcılarını yetkilendirin

Kullanıcıların sunucuda önceden mevcut olması gerekir:

```bash
make admin-add
make admin-add ADMIN_USER=baska-kullanici
make admin-list
```

Linux hesabını silmeden panel erişimini kaldırmak için:

```bash
make admin-remove ADMIN_USER=baska-kullanici
```

Uygulama grup üyeliğini her girişte kontrol eder; servis restart gerekmez.
Yetkili grup kaydını şu komutla doğrulayın:

```bash
getent group system-maintenance-admin
```

## 4. Giriş yapın

`ALLOWED_CIDRS` tarafından izin verilen bir istemciden
`https://SUNUCU:8443` adresini açın. Linux kullanıcı adını ve hesabın
yerel/PAM parolasını kullanın.

SSH private key web parolası değildir. Yalnızca public-key SSH için ayarlanmış
hesap, varsayılan web girişi için geçerli bir PAM doğrulama yöntemine ihtiyaç duyar.

Doğrulama UID kontrollü Unix socket üzerinden root agent tarafından yapılır.
Yetkisiz web süreci `/etc/shadow` okumaz; parolalar saklanmaz ve loglanmaz.

## 5. Giriş sorunlarını teşhis edin

```bash
make admin-list
sudo passwd -S KULLANICI
sudo journalctl -u system-maintenance-agent.service \
  -u system-maintenance-monitor.service -n 100 --no-pager
```

- `authentication failure`: Linux parolasını ve PAM ayarını doğrulayın.
- `account is not authorized`: admin grup üyeliğini doğrulayın.
- `temporarily_banned`: hatalı denemelerden sonra belirtilen ban süresini bekleyin.
- Agent erişilemiyor: `make status` ve `make logs` kullanın.

Kimlik doğrulama kodu/ayarı güncellendiyse `make upgrade` çalıştırın. Yalnızca
grup üyeliği değişikliklerinde restart gerekmez.

## 6. Sertifika ve ağ

Kendi sertifikanıza geçmek için:

```bash
make cert-install CERT=/path/fullchain.pem KEY=/path/privkey.pem
```

Erişim ağını değiştirmek için `/etc/system-maintenance/monitor.conf` içindeki
`ALLOWED_CIDRS` değerini düzenleyip `make restart` çalıştırın.

## 7. Özellikleri yapılandırın

1. **Alarmlar → Telegram** altında hedef ekleyip test gönderin.
2. **Alarmlar → Kurallar** altında eşik tanımlayın.
3. Servis, koruma ve job executable izin listelerini gözden geçirin.
4. **Zamanlayıcılar** altında yönetilen görev oluşturun.
5. Kalıcı işlem geçmişi için **İşlemler → Takip kuralı** kullanın.
6. **Ağ** ekranında muhasebe rozetinin **EXACT eBPF** olduğunu doğrulayın.
   **DEGRADED** çalışır ancak kayıpsız muhasebe şartını karşılamaz.
