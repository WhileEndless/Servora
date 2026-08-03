# Servora

<a href="https://github.com/WhileEndless/Servora"><img src="web/public/assets/servora-logo.png" alt="Servora logosu" width="140"></a>

Servora, Linux için kendi sunucunuzda çalışan izleme ve
yönetim panelidir. **0.0.1** sürümü, depodaki mevcut paket/Docker bakım
zamanlayıcısını güvenli ve geçmiş tutan sistem monitörüyle birleştirir.

[GitHub](https://github.com/WhileEndless/Servora) · [English README](README.md) · [Kurulum](docs/tr/installation.md) · [Mimari](docs/tr/architecture.md) ·
[Güvenlik](docs/tr/security.md) · [Operasyon](docs/tr/operations.md)

## Özellikler

- CPU, load, RAM, swap, disk ve ağ için saniyelik canlı metrikler, geçmiş kayıtlar
  ve veri güncelliği göstergeleri.
- Sıralanabilir htop benzeri işlem listesi, işlem ayrıntıları, kalıcı regex takip
  kuralları ve onay isteyen kontrollü sinyaller.
- `/proc` üzerinden zaten okunan telemetriyi kullanan, isteğe bağlı süreç/grup
  CPU, yerleşik bellek ve disk I/O geçmişi.
- systemd servis envanteri, çalışma süresi, kaynak verileri ve izinli eylemler.
- APT/dpkg ve DNF/RPM için kurulu paket, sürüm, güncelleme adayı, dosya yolu
  envanteri ve kurulum/kaldırma/sürüm değişikliği geçmişi.
- Docker sağlık, port, CPU, RAM, ağ ve blok I/O bilgileri.
- Docker container görünümünden ayrı, tag/digest, boyut ve kullanım ilişkili
  yerel image envanteri.
- Süreç, süreç grubu ve uzak uç bazında kesin TCP/UDP uygulama baytı
  muhasebesi; aranabilir geçmiş ve ayarlanabilir saklama süresi.
- Aktif SSH oturumları, systemd timer ve klasik cron envanteri.
- İzinli executable'larla güvenli, uygulama tarafından yönetilen systemd timer'ları.
- Panel alarm kuralları ve birden fazla Telegram hedefi.
- Linux/PAM girişi, kalıcı oturum, CSRF koruması, CIDR kısıtı, kademeli
  giriş banı ve denetim kaydı.
- Ana dili İngilizce, Türkçe seçeneği bulunan Vue 3 arayüz.
- Tarayıcı geri/ileri desteği bulunan URL tabanlı sayfalar ve sol menüde canlı
  CPU, RAM, ağ ve uptime özeti.

Paket içeriği yakalanmaz. Keyfi shell endpoint'i bulunmaz. Mevcut cron girdileri
ve uygulama tarafından yönetilmeyen systemd unit'leri salt okunurdur.

Ağ ekranı yakalama durumunu açıkça gösterir. **EXACT eBPF**, bir baytlık
aktarımlar dahil başarılı her uygulama baytını sayar. **DEGRADED** modu socket
sayaçlarını örnekler ve kısa aktarımları kaçırabilir. Kapasite nedeniyle
kaybolan bayt varsa ayrıca sayılır ve hata olarak gösterilir; kayıp gizlenmez.
Bu değerler Ethernet çerçevesi/TCP retransmission değil, kernelin kabul ettiği
veya uygulamaya döndürdüğü uygulama baytlarıdır.
Trafik ve kaynak analizlerinde ikili birimler (`KiB`, `MiB`, `GiB`, `TiB`)
kullanılır.

## Gereksinimler ve kurulum

Linux, systemd, PAM, Go 1.26.5+, C derleyicisi, PAM/SQLite geliştirme başlıkları,
Node.js 22+, npm ve OpenSSL gerekir. Docker isteğe bağlıdır.

Debian/Ubuntu:

```bash
sudo apt-get install golang-go gcc clang llvm libbpf-dev bpftool \
  libpam0g-dev libsqlite3-dev nodejs npm openssl
```

```bash
make test
make build
make install
make cert-generate HOSTS="203.0.113.10,monitor.example.lan"
make start
```

Tüm desteklenen akışları görmek için istediğiniz zaman `make help` çalıştırın.

`make install`, sudo komutunu çağıran kullanıcıyı
`system-maintenance-admin` grubuna ekler. Üyelik yeni oturumda etkinleşir.
Panel varsayılan olarak `https://SUNUCU:8443` adresindedir.

Kullanıcı yönetimi:

```bash
make admin-add                         # mevcut Linux kullanıcısı
make admin-add ADMIN_USER=alice        # başka mevcut Linux kullanıcısı
make admin-list
make admin-remove ADMIN_USER=alice
```

Grup değişikliğinden sonra servis restart gerekmez. Web girişi SSH private key
ile değil, Linux hesabının parolasıyla yapılır. Ayrıntılar için
[tam kurulum ve giriş akışına](docs/tr/installation.md) bakın.

Kendi sertifikanıza geçmek için:

```bash
make cert-install CERT=/path/fullchain.pem KEY=/path/privkey.pem
```

Kurulu ayarlar `/etc/system-maintenance/monitor.conf`, geçmiş veriler
`/var/lib/system-maintenance-monitor` altındadır. Erişim ağını `make install`
sırasında onayladığınız değer belirler; cevap verilmezse yalnızca loopback
açılır, yani servis adını koymadığınız bir ağa hiçbir zaman açılmaz. Veri kotası
2 GiB, ham saklama 30 gün ve özet saklama bir yıldır.

Hızlı sistem metrikleri varsayılan olarak saniyede bir akar. Bağlantı sahipliği,
Docker ve işletim sistemi envanterleri ayrı arka plan aralıklarında yenilendiği
için yavaş bir isteğe bağlı toplayıcı canlı paneli durdurmaz. Farklı bir hız için
`monitor.conf` içinde en az `500ms` olacak şekilde `SAMPLE_INTERVAL` ayarlanabilir.

## Yaşam döngüsü

```bash
make status
make logs
make restart
make stop
make upgrade
make uninstall  # ayar, sertifika ve geçmişi korur
make purge      # korunan uygulama verilerini kalıcı siler
```

Geliştirme, güvenlik ve genişletme ayrıntıları `docs/tr/` altındadır. İngilizce
dokümanlar kanonik sürümdür; Türkçe dosyalar bunların eşlik eden çevirisidir.

## Lisans

Servora, [GNU Affero General Public License v3.0](LICENSE) ile lisanslanmıştır.
