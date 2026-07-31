# Servora API kuralları

Tüm endpoint'ler `/api/v1` altındadır ve varsayılan temsil JSON'dur. Liste
cevapları `items` dizisi kullanır. Hatalar kararlı `error.code` ve
`error.message` alanları taşır.

`POST /auth/login` ve `GET /health` dışındaki rotalar güvenli oturum cookie'si
ister. POST/DELETE çağrıları ayrıca `X-CSRF-Token` ve istek host'uyla eşleşen
HTTPS `Origin` gerektirir.

Ana kaynaklar `overview`, `history`, `processes`, `watches`, `network`, `ssh`,
`docker`, `services`, `schedules`, `alert-rules`, `alerts`,
`notification-targets`, `activities`, `modules` ve `actions`'tır. Canlı
snapshot'lar `/stream` üzerinden server-sent event olarak gelir.

CPU, RAM, işlem ve interface metrikleri varsayılan olarak her saniye yayınlanır.
Bağlantı sahipliği bir, Docker beş, systemd/SSH/timer envanteri on beş saniyede
bir yenilenir. Ağır envanterler cache'lendiği için hızlı telemetriyi durdurmaz.

`GET /docker`; erişilebilirlik, yenileme zamanı, toplayıcı hataları, container
listesi ve daemon özeti döndürür. Özet; engine sürümü, storage driver, image
sayısı ile çalışan/durmuş/duraklatılmış container sayılarını içerir. Daemon
erişilebilirken boş `items` dizisi geçerli bir boş envanterdir.

`GET /docker/images`, yerel image'ları fiziksel image ID başına bir kayıt
olarak; tag/repository referansları, digest'ler, boyut ve kullanan container'lar
ile döndürür. Uzak registry sorgulamaz.

`GET /packages`, kurulu sistem paketlerini arama, güncelleme durumu, sıralama ve
sayfalama parametreleriyle döndürür. `GET /packages/{id}/files` paket dosyalarını
sayfalar, `GET /package-events` kurulum/kaldırma/sürüm değişikliği geçmişini
verir. `POST /packages/refresh` paket kurmaz; yalnız repo metadatasını yenileyen
arka plan kontrolünü başlatır ve denetim kaydı üretir.

`GET /processes/{pid}`; executable, çalışma dizini, cgroup, kernel status,
limitler, child PID'ler, namespace, açık dosya özeti ve işlem ağ bağlantılarını
döndürür.

`GET /network-usage`; `from`, `to`, `group_by=process|group` ve `q`
parametrelerini kabul eder. Bayt toplamlarını, hedef sayılarını, etkinlik
aralığını ve depolama kullanımını döndürür. `GET /network-usage/detail` aynı
zaman aralığına ek olarak `selector=process|group|pid` ve `value` alır; uzak uç
toplamlarını ve zaman dilimli etkinliği döndürür.
Canlı süreç detay paneli yalnız açıkken bu seçiciyi saniyede bir kullanır; panel
kapatıldığında ek istek yapılmaz.

`GET /resource-usage` aynı zaman, gruplama ve arama parametrelerini alır;
ortalama/tepe CPU ve yerleşik bellek ile disk okuma-yazma deltalarını döndürür.
`GET /resource-usage/detail` seçilen süreç veya grubun zaman serisini verir.

`GET /settings/network` saklama ve depolama bilgisini verir.
`PATCH /settings/network`, 1–365 arası `{"retention_days":10}` kabul eder.
`DELETE /settings/network` için `{"confirm":"DELETE NETWORK HISTORY"}` gerekir
ve kayıtlı akış geçmişiyle canlı eBPF akış/bütünlük sayaçlarını kalıcı olarak
temizler. Değişiklik çağrıları CSRF ile
korunur ve denetim kaydına yazılır.
Toplayıcı anahtarları `network_enabled`, `cpu_enabled`, `memory_enabled` ve
`disk_io_enabled` alanlarıdır. `DELETE /settings/resources` tam olarak
`DELETE RESOURCE HISTORY` onay metnini gerektirir.

API'de genel komut çalıştırma rotası bulunmaz; istemciler yalnızca agent'ın
kabul ettiği tiplenmiş eylemleri kullanabilir.
