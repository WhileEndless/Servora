# Servora güvenlik modeli

- PAM sonrasında yalnızca `system-maintenance-admin` üyeleri kabul edilir.
- Parolalar saklanmaz. Oturum cookie'si rastgele opaque değerdir; veritabanında
  yalnızca SHA-256 hash'i tutulur.
- Değişiklikler oturum, aynı origin ve CSRF token doğrulaması gerektirir.
- Uzun süreli SSE telemetrisi idle süresini yenilemeden tekrar doğrulanır; süresi
  dolan veya silinen oturum stream'i kapatır ve tarayıcıdaki korumalı veriyi temizler.
- 15 dakikada beş hata 30 dakikalık IP banı oluşturur; tekrarında süre katlanır.
- Doğrudan bağlantı adresi `ALLOWED_CIDRS` içinde olmalıdır.
- Servisler ve zamanlanmış executable'lar açık izin listeleriyle sınırlandırılır.
- Telegram token'ları izinleri kısıtlı dosyalardadır ve API'den geri dönmez.
- Reboot/shutdown hem arayüzde hem agent tarafında kesin onay değeri ister.

Admin grup üyeliğini sunucu yönetici yetkisi olarak değerlendirin. Paneli
LAN/VPN'de tutun, güvenilir TLS sertifikası kullanın ve izin listelerini kısa tutun.
