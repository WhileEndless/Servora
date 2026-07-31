# Servora mimarisi

Ürün iki çalışma zamanı güven bölgesine ayrılır.

`system-maintenance-monitor`, yetkisiz `system-maintenance` hesabıyla çalışır.
Gömülü Vue arayüzünü sunar, PAM doğrulamasını, SQLite verilerini ve alarm
değerlendirmesini yönetir.

`system-maintenance-agent` root çalışır. Unix socket'ine yalnızca ayrı agent
grubu erişebilir. Protokol tiplenmiş metrik okumaları ve sabit, doğrulanan
eylemleri kabul eder; komut metni kabul etmez.

Backend collector, action, alert-source ve notifier sorumluluklarına ayrılmıştır.
Vue view bileşenleri doğrudan `fetch` çağırmaz; HTTP/CSRF davranışı `ApiClient`,
oturum ve telemetri durumu `MonitorStore` tarafından yönetilir.

## Ağ muhasebesi

Yetkili agent TCP/UDP gönderme ve alma yollarına CO-RE eBPF giriş/dönüş
probları bağlar. Yalnızca pozitif dönüş değerleri sayıldığı için sayaçlar
başarılı uygulama baytlarını gösterir. PID, komut, protokol ve uzak uç metadata
olarak tutulur; paket içeriği okunmaz.

Sayaçlar atomik olarak SQLite'a alınır ve bir dakikalık dilimlerde toplanır.
262.144 anahtarlı harita eski veriyi sessizce atmaz. Kapasite aşılırsa kayıp
olay ve baytlar ayrı sayaçta tutulur; arayüz veri bütünlüğü hatası gösterir.
eBPF yüklenemezse kullanılan `socket-counter-fallback`, kısa bağlantıları
garanti edemediğinden açıkça **DEGRADED** olarak işaretlenir.
