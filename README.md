# Smart Campaign Composer

Pazarlamacının doğal dille yazdığı kampanya isteğini (brief) Claude ile **doğrulanmış bir hedef kitle kuralına** ve **SMS + email içeriğine** çeviren, insan onayına sunan, mesajları Postgres tabanlı bir kuyruk üzerinden mock provider'a gönderen ve gönderimi canlı izleten uçtan uca sistem. Backend Go, frontend Vue 3 + TypeScript, veritabanı ve kuyruk PostgreSQL 16. Gerçek kimseye mesaj gitmez: alıcılar 5.000 sahte kullanıcı, gönderim mock provider'a yapılır.

Kararlar ve gerekçeleri: [DECISIONS.md](DECISIONS.md) · Tasarım dokümanı: [docs/superpowers/specs/](docs/superpowers/specs/2026-10-29-smart-campaign-composer-design.md) · Case metni: [docs/brief.md](docs/brief.md)

## Hızlı başlangıç

```bash
cp .env.example .env        # isteğe bağlı; ANTHROPIC_API_KEY'i buraya yazın
docker compose up --build
```

| Adres | Ne |
|---|---|
| http://localhost:3000 | Arayüz (composer, kampanya listesi, canlı dashboard) |
| http://localhost:8080 | API (`/api/...`, `/healthz`, `/readyz`, `/metrics`) |
| http://localhost:8081 | Mock SMS/email provider (`/v1/stats`, `/healthz`) |
| http://localhost:9091, :9092 | İki worker replikasının `/metrics` ve `/healthz` uçları |

`api` açılışta migration'ları çalıştırır ve veritabanı boşsa deterministik seed'i yükler (5.000 kullanıcı, ~24.000 event). Servisler healthcheck'lerle sırayla ayağa kalkar: postgres → api (migrate + seed) ve mock-provider → 2 worker + web.

**LLM seçimi** (sessiz fallback yok):

- **Gerçek Claude:** `.env` içine `ANTHROPIC_API_KEY=...` yazın. Modeller `ANTHROPIC_MODEL_SEGMENT` / `ANTHROPIC_MODEL_CONTENT` (varsayılan `claude-sonnet-5-5`).
- **Key yoksa:** sistem yine ayağa kalkar, ama LLM endpoint'leri `503 llm_not_configured` döner ve arayüzde kırmızı uyarı bandı görünür. Sistem kendiliğinden mock'a **düşmez**.
- **Key'siz demo:** `LLM_PROVIDER=mock docker compose up` (veya `.env` içinde `LLM_PROVIDER=mock`). Deterministik, anahtar kelime tabanlı fixture'lar çalışır; arayüzde kalıcı turuncu **MOCK MOD** bandı çıkar ve kampanyalar `llm_provider='mock'` olarak işaretlenir.

Hızlı kontrol: arayüzde örnek brief'i ("Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun.") girip kitle → içerik → onay adımlarını izleyin. Mock modda bu kampanya 648 kişilik kitleye 1.091 mesaj (541 SMS + 550 email) üretir; varsayılan hata oranlarıyla ≈40 sn'de `completed` olur, retry'lar dashboard'un "Son hatalar" kartında görünür ve `curl localhost:8081/v1/stats` → `"max_deliveries_per_recipient_message": 1`.

`make up` aynı şeyi arka planda yapar ve tüm servisler sağlıklı olunca döner. Kapatmak için: `docker compose down -v` (veya `make down`) (`-v` veritabanı volume'unu da siler; bir sonraki açılışta seed yeniden yüklenir).

## Akış

| Adım | Ne olur | Ne garanti edilir |
|---|---|---|
| 1. Kitle | Claude brief'ten JSON segment kuralı üretir; arayüz kural satırlarını, açıklamayı, kanal bazında kişi sayılarını ve uyarıları gösterir. Doğal dille düzeltme ("Ankara'yı da ekle") eski/yeni kuralı ve sayı farkını yan yana gösterir. | Kural şema + semantik doğrulamadan geçer, parametreli SQL'e derlenir; LLM çıktısı SQL olarak çalışmaz. Brief'te karşılığı olmayan ifadeler `unmapped` uyarısı olarak görünür; 0 kişilik kitle onaylanamaz, kitle tüm kullanıcıların %50'sini aşarsa ek onay kutusu zorunludur. |
| 2. İçerik | SMS ve email (konu, preheader, gövde) SSE ile alan alan akar. Kanal bazında yeniden üretim var. | SMS ≤ 160, konu ≤ 80 karakter; uzunluk kitledeki **en uzun** isim/şehirle ve **boş** isimle render edilerek ölçülür. Yalnızca `{{first_name}}`, `{{city}}` değişkenleri kabul edilir. GSM-7 dışı karakterde UCS-2 parça sayısı uyarısı gösterilir. |
| 3. Onay | Pazarlamacı önizler (gerçek bir alıcıyla ve isimsiz render), gerekirse düzenler, kanal kapatabilir, onaylar. | Onay anında kural ve içerik backend'de yeniden doğrulanır. Tek transaction'da yalnızca **o kanalda izni olan** kullanıcılar için mesaj satırları yazılır, içerik alıcı başına render edilip saklanır. `UNIQUE (campaign_id, user_id, channel)` sayesinde çift tıklama çift mesaj üretmez. |
| 4. Gönderim | Worker'lar kuyruktan iş alır, mock provider'a gönderir, hataları sınıflandırıp yeniden dener. | Kanal başına ≤ 100 msg/sn (birden çok worker olsa bile). Aynı kişiye aynı mesaj iki kez gitmez (idempotency key). İzin göndermeden hemen önce tekrar okunur. |
| 5. Canlı izleme | Dashboard ≈2 sn'de bir durum endpoint'ini sorgular: kanal bazında ilerleme, durum dağılımı, saniyelik hız, son hatalar. | Retry'dan sonra gönderilen mesajlar da hata geçmişiyle ve güncel durumlarıyla (`sent`) "Son hatalar"da görünür; `unknown` mesajlar ayrıca açıklanır. |

## Mimari

```
web (nginx + Vue build)  ──►  api (Go)  ──►  postgres
                                  ▲              ▲
                                  │              │
                           worker (Go, 2 adet) ──┘──► mock-provider (Go)
                                  │
                                  └── Claude API (veya açıkça seçilmiş mock LLM)
```

Tek Go binary'si alt komutlarla çalışır: `composer api | worker | migrate | seed`. Mock provider ayrı bir binary ve container.

- **Kuyruk = Postgres tablosu.** `messages` satırları `FOR UPDATE SKIP LOCKED` ile alınır, 30 sn'lik kira (`locked_until`) ile tutulur. Onay + kuyruğa alma tek transaction olduğu için dual-write/outbox sorunu yok. Kirası dolan `sending` satırı (worker göndermenin ortasında öldü) başka worker tarafından "belirsiz" olarak geri alınır.
- **Kanal başına tek lider.** Her kanal için `pg_try_advisory_lock` ile bir lider worker seçilir; 100/sn token bucket yalnızca liderde tutulur, gönderimler goroutine havuzuyla paralel yapılır. Lider ölürse DB oturumu kapanır, kilit düşer, diğer replika devralır. Dağıtık sayaç gerekmez.
- **Idempotency key + belirsiz → `unknown`.** Her istek `Idempotency-Key: <message_id>` taşır. Hatalar "kesin gitmedi" (bağlantı kurulamadı, 429, 4xx) ve "belirsiz" (5xx, timeout) olarak ayrılır. Belirsiz hata, provider idempotency destekliyorsa aynı key ile tekrar denenir; desteklemiyorsa (`PROVIDER_IDEMPOTENCY=false`) mesaj `unknown` olur ve tekrar gönderilmez. 429 deneme hakkından düşmez, kanalı `Retry-After` kadar (en fazla 10 sn) durdurur. Tüm `Mark*` güncellemeleri kira token'ı ile çitlenir: kirası düşmüş eski bir kopya yeni sonucun üzerine yazamaz.
- **İzin iki kez kontrol edilir:** onayda yalnızca izinli kullanıcılar kuyruğa girer; worker iş alırken izni aynı SQL ifadesinde tekrar okur, geri çekilmişse `skipped_no_consent`.
- **DSL → parametreli SQL.** Claude yalnızca izin listesindeki alanlar (`city`, `language`, `abandoned_cart`, `purchased`, `order_count`, `total_spent`, `signed_up`), alana uygun operatörler ve aralık kontrollü değerlerle JSON kural üretebilir. Derleyici değerleri her zaman bind parametresi olarak geçer.
- **Structured output + doğrulama + en fazla 2 onarım.** Claude çıktısı JSON schema ile kısıtlanır, backend'de katı decode + semantik doğrulamadan geçer; hatalar makine-okunur listeyle Claude'a geri gönderilir (en fazla 2 tur).
- **`llm_calls` tablosu.** Her Claude çağrısı (amaç, istek, ham çıktı, doğrulama hataları, deneme no, model, token, gecikme) kaydedilir.
- **Sessiz mock yok.** Key yoksa 503 + kırmızı bant; mock yalnızca `LLM_PROVIDER=mock` ile ve görünür bantla çalışır.

Ayrıntılar ve bedelleri için [DECISIONS.md](DECISIONS.md).

## LLM güvenilirliği

Claude'un çıktısı hiçbir zaman "güvenilir" kabul edilmez; her katman bir öncekinin kaçırdığını yakalar:

1. **Kısıtlı çıktı alanı.** Claude SQL değil, küçük bir segment DSL'i üretir. Şema alan izin listesini, alan başına operatörleri ve değer tiplerini tanımlar; yapılandırılmış çıktı (`output_config.format` JSON schema) ile istenir. İçerik için de kanal bazında şema var (`sms`, `email.subject/preheader/body`).
2. **Backend doğrulaması.** Çıktı bilinmeyen alanları reddeden katı JSON decode'dan, ardından semantik doğrulamadan geçer: bilinmeyen alan/operatör, aralık dışı gün sayısı, bilinmeyen şehir (Türkçe normalizasyonla: `istanbul` → `İstanbul`), çelişen koşullar, boş koşul listesi (`audience: "all"` açıkça seçilmedikçe). İçerikte en kötü durum uzunluğu, izin listesi dışı/bozuk değişkenler (`{{phone}}`, `{{first_name`) kontrol edilir.
3. **Onarım döngüsü.** Doğrulama hataları kod + alan + mesaj listesi olarak ("SMS en kötü durumda 174 karakter, 160'a indir") Claude'a geri gönderilir; en fazla 2 onarım. Sonra hata kullanıcıya gösterilir ve form ile elle düzenleme mümkündür. Refusal, `max_tokens` ile kesilmiş çıktı ve transport hataları onarılmaya çalışılmaz; loglanıp hata olarak döner.
4. **Prompt injection.** Brief, düzeltme talimatı ve mevcut kural etiketli bloklar içinde "güvenilmeyen veri" olarak verilir. Asıl garanti promptta değil doğrulamada: brief ne derse desin çalışan şey yalnızca izin listesindeki koşullardır (`DROP TABLE` içeren alan/değerler testlerde reddediliyor).
5. **Sessiz hata yok.** Brief'in kitleye karşılık gelmeyen kısımları (`"VIP"`, `"genç kullanıcılar"`) `unmapped` olarak döner ve arayüzde uyarı olur; çok geniş kitle ek onay ister.
6. **Akış ve kayıt.** İçerik stream edilirken kısmi JSON ayrıştırılıp alanlar arayüzde dolar; tam çıktı stream sonunda doğrulanır ve yalnızca geçerliyse kaydedilir. Onarım turları SSE'de `repair` olayıyla görünür. Tüm çağrılar `llm_calls` tablosunda ve `/metrics`'te (çağrı, onarım, gecikme).
7. **Test edilebilirlik.** `LLM` interface'inin deterministik mock implementasyonu ve senaryolu (scripted) test istemcisi var; onarım döngüsü, vazgeçme, refusal ve stream kesintisi gerçek API key olmadan test edilir. Gerçek API için ayrı bir duman testi var (`-tags=live`).

Örnek: `docker compose exec postgres psql -U composer -c "select purpose, attempt, validation_errors, latency_ms from llm_calls order by id desc limit 10"`

## Testler

```bash
make test     # test Postgres'i (docker-compose.test.yml, port 55432) kaldırır, Go + frontend testlerini çalıştırır
```

Testler gerçek Postgres kullanır (her test kendi veritabanını açar), gerçek API key gerektirmez. Öne çıkanlar:

| Test | Ne kanıtlar |
|---|---|
| `queue.TestEndToEndExactlyOncePerRecipient` | İki worker, mock provider %10 5xx + %5 429 + %5 kayıp cevap modunda: her alıcıya tam bir teslimat (`max_deliveries_per_recipient_message == 1`), `sent` satırları provider teslimatlarına eşit. |
| `queue.TestRateLimitAcrossWorkers` | Üç worker aynı kanalda: provider herhangi bir 1 sn penceresinde 100'den fazla istek görmez. |
| `queue.TestExpiredLock*`, `TestUncertain*`, `TestNotSentExhaustedIsFailed` | Çökme kurtarma ve hata sınıflandırması: idempotent modda aynı key ile tekrar, değilse `unknown`; kesin gitmeyen hata tükenirse `failed`. |
| `queue.TestStaleLeaseMarkIsNoop`, `TestRevokedConsentSkipped` | Kira çitleme; izin onaydan sonra geri çekilirse mesaj gitmez. |
| `segment.TestAbandonedCartSemantics` | "20 gün önce alıp 5 gün önce sepete ekleyen" kişi dahil `abandoned_cart` tanımı. |
| `campaign.TestApproveConcurrentOnce` | Eşzamanlı onay istekleri tek set mesaj üretir. |
| `campaign.TestApproveSmallPoolNoDeadlock` | 4 bağlantılık havuzda onay patlaması kilitlenmeden biter (onay tek bağlantı kullanır). |
| `segment.TestValidateRejectsUnknownFieldAndRawSQL`, `segment.TestCompileUsesParamsOnly` | Prompt injection/SQL denemeleri doğrulamada reddedilir; derlenen SQL'de değer yoktur, hepsi parametredir. |
| `llm.TestSegment*`, `TestContentRepairsTooLongSMS`, `TestRefusalNotRepaired` | Onarım döngüsü: bir turda düzelir, 2 turdan sonra vazgeçer, refusal onarılmaz. |
| `api.TestFlow`, `api.TestNoKeyReturns503` | HTTP üzerinden uçtan uca akış; key yokken 503. |
| `campaign.TestStatusKeepsErrorOfRetriedThenSentMessage` | Retry sonrası gönderilen mesaj hata geçmişiyle dashboard'da kalır. |
| Frontend (Vitest) | GSM-7/UCS-2 parça hesabı, kısmi JSON ayrıştırma, SSE okuyucu, kural yardımcıları. |

Gerçek Claude ile duman testi (key gerekir, `make test`'e dahil değil):

```bash
cd backend && ANTHROPIC_API_KEY=... go test ./internal/llm -run TestAnthropicSmoke -tags=live -v
```

CI: `.github/workflows/ci.yml` (Postgres service container, `go test ./... -race`, frontend test + build).

## Gözlemlenebilirlik

- `GET /healthz` (liveness) ve `GET /readyz` (DB erişimi; `llm: ok|mock|not_configured` bilgisi) — api'de. Worker'larda `/healthz` DB'ye ping atar.
- `GET /metrics` (Prometheus) hem api'de (LLM çağrı sayısı/gecikmesi/doğrulama hatası/onarım) hem her worker'da (`composer_messages_total{channel,outcome}`, provider gecikme histogramı, 429 sayacı, limiter'ın o anki izinli hızı `composer_send_rate`; seriler yalnızca o kanalın lideri olan replikada dolar).
- `GET /api/config`: `{llm_provider, llm_configured, models}` — arayüzdeki bantlar buradan.
- `GET /api/campaigns/{id}/status`: durum başına sayılar, kanal kırılımı, 10 sn'lik hız, son 10 hata.
- `llm_calls` tablosu: her Claude çağrısının girdisi, ham çıktısı ve doğrulama hataları.
- Worker logları kanal liderliğinin hangi replikada olduğunu yazar (`acquired channel leadership`).

## Mock provider ayarları

`.env` ile değiştirilebilir (varsayılanlar `.env.example`'da). Oranlar istek başına olasılıktır.

| Değişken | Varsayılan | Davranış |
|---|---|---|
| `MOCK_FAIL_RATE` | 0.05 | 503, mesaj teslim edilmez |
| `MOCK_429_RATE` | 0.03 | Rastgele 429 + `Retry-After: 1`, teslim edilmez |
| `MOCK_LOST_RESPONSE_RATE` | 0.02 | Mesaj teslim edilir, sonra 502 döner ("cevap kayboldu") |
| `MOCK_TIMEOUT_RATE` | 0.01 | Teslim edilir, `MOCK_TIMEOUT_DELAY_MS` bekler, 504 döner (worker 5 sn'de timeout'a düşer) |
| `MOCK_LATENCY_MIN_MS` / `MAX_MS` | 10 / 150 | Her isteğe rastgele gecikme |
| `MOCK_TIMEOUT_DELAY_MS` | 10000 | Timeout senaryosundaki bekleme |
| `MOCK_RATE_PER_SECOND` | 100 | Kanal başına kayan 1 sn penceresi; aşılırsa 429 |
| `MOCK_IDEMPOTENCY` | true | `false`: aynı `Idempotency-Key` tekrar teslim edilir |
| `PROVIDER_IDEMPOTENCY` (worker) | true | Worker'ın provider'ın idempotency desteğine güvenip güvenmediği; `false` iken belirsiz hatalar `unknown` olur |

`GET /v1/stats` → `accepted`, `delivered`, `duplicates_suppressed`, `max_deliveries_per_recipient_message` (aynı alıcıya aynı mesaj kaç kez teslim edildi; 1 olmalı). `GET /v1/messages/{key}` provider tarafındaki durum.

## Kapsam dışı / yapılmayanlar

- **A/B varyantları** (yalnızca kanal bazında "yeniden üret" var), **zamanlanmış gönderim**, **kullanıcının diline göre içerik** (tüm içerik Türkçe; kitlede `en` dilli kullanıcı varsa önizlemede bilgi notu çıkar).
- **Kimlik doğrulama / çoklu kiracı.**
- **Gerçek SMS/email provider entegrasyonu**; yalnızca mock provider.
- **`make eval` ve model karşılaştırması:** golden brief setinin gerçek API ile çalıştırılması ve Sonnet 5.5 / Haiku 5.5 karşılaştırması (onarım oranı, gecikme, metin kalitesi) API key ile yapılacak; sonuç DECISIONS.md #15'e yazılacak. Şu an örnek brief'ler mock LLM fixture'larıyla `make test`'te, gerçek API için yalnızca tek bir duman testi (`-tags=live`) var.

## Bilinen sınırlamalar

- **`abandoned_cart` sadeleştirmesi:** son sepete eklemeden sonra alakasız bir ürün alınmışsa kişi kitleden çıkar; ürün seviyesinde sepet takibi yok.
- **Mock provider'ın idempotency kayıtları bellekte:** provider container'ı yeniden başlarsa sıfırlanır.
- **Efektif tavan ≈95 msg/sn/kanal:** limiter 50 ms güvenlik payı tutar ki provider tarafındaki her 1 sn penceresinde 100 aşılmasın.
- **Liderlik devrinde kısa çakışma:** lider kilit bağlantısını kaybederse, eski liderin uçuştaki (en fazla eşzamanlılık kadar, her biri 5 sn timeout'lu) istekleri yeni liderin ilk istekleriyle çakışabilir; provider'ın 429'u yedek koruma.
- **İzin iş alma anında okunur:** iş alındıktan sonraki milisaniyelerde izin geri çekilirse o mesaj yine gider.
- **Preheader mesaj başına saklanmaz:** `messages` satırında konu ve gövde render edilip saklanır; preheader kampanya içeriğinde kalır ve provider'a gönderilmez.

## Repo yapısı

```
backend/
  cmd/composer/        api | worker | migrate | seed alt komutları
  cmd/mockprovider/    mock SMS/email provider
  internal/
    api/               HTTP API, SSE içerik stream'i, health/config
    campaign/          kampanya store'u, onay (tek tx), durum raporu
    content/           render, en kötü durum uzunluğu, GSM-7/UCS-2, içerik doğrulama
    segment/           DSL, doğrulama, parametreli SQL derleyici, preview
    llm/               LLM interface, Anthropic + mock, onarım döngüsü, kısmi JSON, llm_calls
    queue/             claim (SKIP LOCKED), lider/limiter, dispatcher, hata sınıflandırma, backoff
    mockprovider/      provider sunucusu (idempotency + hata enjeksiyonu)
    seed/              deterministik sahte kullanıcı ve event'ler
    db/, dbtest/, metrics/, verr/, geo/
frontend/              Vue 3 + TS + Vite; nginx.conf (SPA + /api proxy)
docs/                  case brief, tasarım dokümanı, uygulama planı
ai-sessions/           Claude Code oturum export'ları
docker-compose.yml     tüm sistem · docker-compose.test.yml: test Postgres'i
```

## AI araçları kullanımı

Proje Claude Code ile şu döngüyle geliştirildi:

1. **Brainstorm:** gereksinimler ve riskler tartışıldı (ör. `abandoned_cart` tanımındaki hata, "çift gönderim hiç gitmemesinden kötü" kararı, sessiz mock yasağı bu aşamada netleşti).
2. **Spec:** [docs/superpowers/specs/2026-10-29-smart-campaign-composer-design.md](docs/superpowers/specs/2026-10-29-smart-campaign-composer-design.md)
3. **Plan:** [docs/superpowers/plans/2026-10-29-smart-campaign-composer.md](docs/superpowers/plans/2026-10-29-smart-campaign-composer.md) — görevlere bölünmüş uygulama planı.
4. **Uygulama:** her görev bir alt ajan tarafından testle birlikte yazıldı, ardından bağımsız bir review ajanı tarafından incelendi; bulunan sorunlar ayrı `fix:` commit'leriyle düzeltildi (git geçmişinde görülebilir: kira çitleme, onayda havuz kilitlenmesi, stream sırası vb.).

Oturum export'ları `ai-sessions/` altında (API key ve kişisel bilgiler temizlenmiş).
