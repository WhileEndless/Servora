# Servora modülü ekleme

Modül kodunu dar bir arayüz arkasında tutun ve ilgisiz handler/view'lardan
import etmek yerine uygulama birleştirme katmanında kaydedin.

- Collector tiplenmiş snapshot ile capability/health durumu döndürür.
- Action provider root sınırına girmeden önce kimlik ve argümanları doğrular.
- Alert source, kural motorunu toplama ayrıntısına bağlamadan değer/olay sunar.
- Notifier hazırlanmış olayı alır ve teslim sonucunu döndürür. Secret'lar
  kimlikle referans edilir, kural kaydına gömülmez.

İsteğe bağlı bağımlılıklar çalışma zamanında algılanmalı; süreç kapanmak yerine
unavailable capability göstermelidir. Doğrulama, fallback ve secret redaksiyonu
testlerini ve iki dilde dokümanı ekleyin.

İlk sürüm bilinçli olarak derleme zamanı modülleri kullanır; keyfi shared object
veya executable plugin olarak yüklenmemelidir.
