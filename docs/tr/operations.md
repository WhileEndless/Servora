# Servora operasyonları

## Telegram

BotFather ile bot oluşturun, gerekiyorsa hedef gruba ekleyin ve chat ID'yi alın.
**Alarmlar → Telegram** bölümünde hedefi kaydedip test mesajı gönderin. Token
kaydedildikten sonra yalnızca yazılabilir; tekrar gösterilmez. Hedef kimliğini
kullanmak yerine alarm oluştururken isimlendirilmiş hedeflerden bir veya
birkaçını seçin.

## Zamanlanmış işler

`system-maintenance.service`, `Type=oneshot` birimidir. Bakım tamamlanınca servis
çıktığı ve `system-maintenance.timer` sonraki çalışmayı beklediği için iki çalışma
arasında `inactive (dead)` görünmesi normaldir. Servisler ekranı bu durumu
hazır/timer bekleniyor olarak gösterir ve **şimdi çalıştır** eylemini sunar.

Tam executable yolunu `JOB_EXECUTABLES` ayarına ekleyip agent'ı yeniden başlatın.
Arayüz systemd `OnCalendar` ifadelerini kabul eder. Oluşturulan unit'ler
`system-maintenance-job-` öneklidir, aynı anda ikinci kez başlamaz ve bir saat
timeout kullanır. Mevcut cron görevleri ve yönetilmeyen timer'lar salt okunurdur.

## Sorun giderme

```bash
make status
make logs
journalctl -u system-maintenance-agent.service --since today
journalctl -u system-maintenance-monitor.service --since today
```

Giriş başarısızsa yeni oturum açtıktan sonra `id KULLANICI` ile grup üyeliğini
doğrulayın. Docker görünürlüğü çalışan Docker daemon gerektirir; web servisi
bilinçli olarak Docker grubuna eklenmez.

Docker ekranı daemon erişilemiyor durumunu, daemon'a bağlı ancak container
bulunmaması durumundan ayırır. İkinci durumu `docker ps -a` ile doğrulayın.

## Paket envanteri

**Paketler → Envanter** APT/dpkg veya DNF/RPM paketlerini ve güncelleme
adaylarını gösterir. İlk tarama baz çizgisidir; sonraki kurulum, kaldırma ve
sürüm değişimleri **Değişimler** altında bir yıl tutulur. Paket ayrıntısından
paketin dosya yolları aranabilir.

**Güncellemeleri kontrol et** paket kurmaz. Yalnız paket deposu metadatasını
yeniler; paket yöneticisi kilitliyse son başarılı veri korunur ve hata
arayüzde gösterilir. Tarama ve metadata aralıkları `monitor.conf` içindeki
`PACKAGE_*` ayarlarıyla değiştirilebilir.

Temel telemetri varsayılan olarak saniyede bir yenilenir. `SAMPLE_INTERVAL`
`500ms` ile `1m` arasını kabul eder; değişiklikten sonra servisleri restart edin.

## Ağ geçmişi ve veri bütünlüğü

Ağ ekranı varsayılan olarak süreç bazında toplamları gösterir. **Gruplar** ile
SSH gibi ilişkili süreçleri birleştirebilir, süreç/grup/kullanıcı arayabilir ve
satıra tıklayarak zaman çizelgesiyle uzak uçları inceleyebilirsiniz.

Eksiksizlik gerektiğinde mod rozeti **EXACT eBPF** olmalıdır. **DEGRADED**
görünüyorsa agent logunu ve nesne dosyasını kontrol edin:

```bash
make logs
ls -l /opt/system-maintenance/lib/network_accounting.bpf.o
```

Debian/Ubuntu derleme bağımlılıkları ve yükseltme:

```bash
sudo apt-get update
sudo apt-get install -y clang llvm libbpf-dev bpftool
make upgrade
```

Varsayılan saklama süresi on gündür; **Ayarlar → Ağ geçmişi** bölümünden
değiştirilebilir. Kapasite kaynaklı tek bir bayt bile kaybolursa arayüz bunu veri
bütünlüğü hatası olarak gösterir.
Ağ geçmişini temizleme işlemi canlı eBPF akış map'lerini ve kayıp bayt bütünlük
sayacını da sıfırlar; sonraki örnek yeni bir hesaplama dönemi başlatır.

## CPU, bellek ve disk I/O geçmişi

Geçmiş CPU, yerleşik bellek, disk okuma ve yazma değerlerini aramak/gruplamak
için **İşlemler → Kaynak analizi** bölümünü kullanın. CPU ve bellek
ortalama/tepe, disk değerleri sayaç deltası olarak tutulur. Disk toplayıcı
kapalıyken oluşan I/O sonradan hesaba katılmaz.

Kaynak satırı açıldığında saklanan ağ hedefleri de birleştirilir; eşleşen süreç
hâlâ çalışıyorsa executable, çalışma dizini, cgroup ve açık dosya yolları yalnızca
istek anında okunur. Açık yollar canlı kanıttır, tarihsel dosya başına bayt
iddiası değildir. Syscall seviyesinde sürekli dosya takibi disk yoğun
sistemlerde yüksek olay hacmi oluşturabileceği için bilinçli olarak etkin değildir.

**Ayarlar → Geçmiş kaynak toplayıcıları** ağ, CPU, bellek ve disk I/O
kalıcılığını ayrı ayrı yönetir. Kalıcılığı kapatmak canlı telemetriyi kapatmaz.
Kaynak satır sayısı/boyutu görülebilir ve geçmiş ayrı onay metniyle
temizlenebilir. Tüm seriler yapılandırılan 1–365 günlük saklama süresini paylaşır.

Alarm olayları önem seviyesine göre renklendirilir. CPU/bellek/load olaylarında
en yüksek süreçler, disk kapasitesi olaylarında en dolu mount, ağ toplamı
olaylarında en çok trafik kullanan süreçler gösterilir.
