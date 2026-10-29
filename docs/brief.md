# Smart Campaign Composer — Case Brief

Insider One · AI Developer Case Study · 2026 · Teslim: 7 gün

Pazarlamacının doğal dille yazdığı bir kampanya isteğini Claude ile hedef kitleye ve SMS ve email mesajlarına çeviren; onaya sunan ve gönderip durumunu canlı gösteren uçtan uca bir sistem.

**Frontend** Vue.js · **Backend** Go veya PHP · **LLM** Claude API

## Kullanıcı akışı

Örnek brief: “Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun.”

1. **Kitle çıkarılır:** Claude brief'ten bir segment kuralı üretir, arayüz kaç kişiye gideceğini gösterir.
2. **Mesajlar üretilir:** SMS ve email içeriği ekrana stream edilerek akar.
3. **İnsan onaylar:** Pazarlamacı önizler, gerekirse düzenler ve onaylar.
4. **Gönderilir:** Mesajlar kuyruk üzerinden mock provider'a gönderilir.
5. **Canlı izlenir:** Dashboard gönderim durumunu gösterir.

Gerçek kimseye mesaj gitmez; alıcılar sahte kullanıcılar, gönderim mock provider'a.

## Fonksiyonel gereksinimler

- **Kitle:** Claude'un ürettiği segment kuralı backend'de doğrulanır; LLM çıktısı doğrudan SQL olarak çalıştırılmaz. İzni olmayan kullanıcıya o kanaldan mesaj gönderilmez.
- **İçerik:** SMS en fazla 160 karakter, email konusu en fazla 80 karakter. `{{first_name}}` gibi kişiselleştirme değişkenleri desteklenir.
- **Gönderim:** Asenkron kuyruk, kanal başına saniyede en fazla 100 mesaj. Aynı kişiye aynı mesaj iki kez gitmez. Retry stratejisi bizim tasarımımız.
- **Takip:** Canlı gönderim durumu, metrics ve health check endpoint'leri.

## AI gereksinimleri (case'in en önemli kısmı)

- **Doğrulama:** Claude'un çıktısı yapılandırılmış (JSON) olmalı ve backend'de kontrol edilmeli.
- **Test edilebilirlik:** Testler gerçek API key olmadan, mock bir LLM ile çalışmalı.

## Kaynaklar

- `ANTHROPIC_API_KEY` `.env`'den okunur; repoda `.env.example` bulunur.
- Sahte kullanıcı verisi: birkaç bin kullanıcılık seed (alanlar bizim kararımız).
- Mock SMS/email provider: zaman zaman hata, gecikme ve 429 döndürmeli.

## Teslim edilecekler

- GitHub/GitLab repository
- `docker compose up` ile tek komutla kurulum (seed ve mock provider dahil)
- `make test` ile çalışan testler
- `README.md` (kurulum + kısa mimari özeti), `DECISIONS.md` (kararlar ve nedenleri)
- `ai-sessions/`: Claude session export'ları (API key ve kişisel bilgiler temizlenmiş)

Kapsam notu: Her şeyi yapmak yerine çekirdek akışı sağlam yapmak tercih edilir; yetişmeyenler README'de belirtilir.

## Bonus (isteğe bağlı)

Zamanlanmış gönderim · A/B varyantları · Kullanıcının diline göre içerik · CI pipeline

## Değerlendirme

LLM entegrasyonu %25 · Mimari ve kararlar %20 · Backend güvenilirliği %20 · Frontend %20 · Test ve kod kalitesi %15
