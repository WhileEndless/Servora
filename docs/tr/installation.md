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

Derlemeden önce araç zincirini doğrulayın. `make deps-check` eksik komut ve
geliştirme başlıklarının tamamını tek seferde, kurulum komutuyla birlikte
bildirir:

```bash
make deps-check
make help
make test
```

## 2. Kurun ve başlatın

```bash
make setup
```

Bu akış Vue ve Go kodunu derler; servis kullanıcı/gruplarını, PAM/systemd
dosyalarını kurar; self-signed sertifika üretir ve servisleri etkinleştirir.
`sudo` çağıran kullanıcı otomatik yetkilendirilir.

Kurulum, sistemde hiçbir değişiklik yapmadan önce makineden makineye değişen üç
ayarı sorar. Köşeli parantez içindeki değeri kabul etmek için Enter'a basın:

- **Dinlenecek adres:port** — varsayılan `0.0.0.0:8443`. Portu başka bir servis
  tutuyorsa kurulum o süreci adıyla bildirir ve bir sonraki boş portu önerir;
  başlayamayacak bir servis bırakmaz. Portu zaten çalışan Servora monitörü
  tutuyorsa bu bir çakışma sayılmaz.
- **Erişime izinli ağlar** — `ALLOWED_CIDRS`. Varsayılan, bu makinenin kendi
  arayüzlerinden türetilir; container ve sanal köprüler hariç tutulur, loopback
  her zaman eklenir.
- **Sertifika adresleri** — tarayıcıda kullanılacak tüm IP/DNS adları. Yalnızca
  henüz sertifika yoksa sorulur.

Terminal olmadan cevaplamak için değerleri doğrudan geçin. Böyle verilen ayar
sorulmaz; CI ve `curl | sudo bash` kurulumları bu yolu kullanır:

```bash
make install LISTEN=0.0.0.0:9443 ALLOWED_CIDRS=203.0.113.0/24 \
  HOSTS="203.0.113.10,monitor.example.lan"
```

Aşamalı kurulum:

```bash
make build
make install
make start
```

`make install` tekrar çalıştırılabilir. Mevcut cevaplar varsayılan olarak geri
gelir ve `monitor.conf` içinde yalnızca değiştirdiğiniz anahtarlar yeniden
yazılır; yorumlar ve elle yaptığınız düzenlemeler korunur, `.bak` yedeği alınır.

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
