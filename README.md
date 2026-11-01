# Smart Campaign Composer

Pazarlamacının doğal dille yazdığı kampanya isteğini (brief) Claude ile **doğrulanmış bir hedef kitle kuralına** ve **SMS + email içeriğine** çeviren, insan onayına sunan, mesajları Postgres tabanlı bir kuyruk üzerinden mock provider'a gönderen ve gönderimi canlı izleten uçtan uca sistem. Backend Go, frontend Vue 3 + TypeScript, veritabanı ve kuyruk PostgreSQL 16. Gerçek kimseye mesaj gitmez: alıcılar 5.000 sahte kullanıcı, gönderim mock provider'a yapılır.

Kararlar ve gerekçeleri: [DECISIONS.md](DECISIONS.md) · Tasarım dokümanı: [docs/superpowers/specs/](docs/superpowers/specs/2026-10-29-smart-campaign-composer-design.md) · Case metni: [docs/brief.md](docs/brief.md)

## Hızlı başlangıç

**Gereksinimler:** `docker compose up` için yalnızca Docker (Compose v2). `make test` için ek olarak Go ≥ 1.24 ve Node 22.

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

Hızlı kontrol: arayüzde örnek brief'i ("Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun.") girip kitle → içerik → onay adımlarını izleyin. Mock modda bu kampanya 648 kişilik kitleye 1.091 mesaj (541 SMS + 550 email) üretir; varsayılan hata oranlarıyla ≈40 sn'de `completed` olur, retry'lar dashboard'un "Gönderim günlüğü" kartında görünür ve `curl localhost:8081/v1/stats` → `"max_deliveries_per_recipient_message": 1`.

`make up` aynı şeyi arka planda yapar ve tüm servisler sağlıklı olunca döner. Kapatmak için: `docker compose down -v` (veya `make down`) (`-v` veritabanı volume'unu da siler; bir sonraki açılışta seed yeniden yüklenir).

## Arayüz

Sol menülü tek sayfa uygulama: kampanya listesi (kampanya adı + brief, durum ve kanal rozetleri, özet sayılar) ve 5 adımlı composer (Brief → Kitle → İçerik → Onay → Gönderim). Kampanyanın adı brief'ten otomatik türetilir (ilk ~60 karakter, kelime sınırında kesilip "…" eklenir) ve composer ile dashboard başlığında yerinde düzenlenebilir. Her adım geçişinde sayfa en üstten başlar; ulaşılamayan adımlar adım göstergesinde kapalıdır. Kitle adımında koşul formu, Claude'un açıklaması, canlı kişi sayıları ve örnek kişiler; **Kitleyi gör** ile kurala uyan tüm kişiler (ad, şehir, dil, SMS/e-posta izni, sipariş sayısı, toplam harcama) sayfa başına 50 kişilik, isimle aranabilir bir çekmecede listelenir. İçerik adımında solda değişken etiketli editörler, uzunluk ve SMS parça göstergesi; sağda kaydırırken görünür kalan tek bir telefon/gelen kutusu önizlemesi (SMS / E-posta ve örnek kişi / isimsiz geçişli; dar ekranda alt alta). Gönderim adımında genel ilerleme, kanal bazında ilerleme çubukları ve saniyelik hız, sabit boyutlu ve kaydırılabilir **Gönderim günlüğü** (yeniden denemeler ve hatalar, okunur nedenlerle: "Zaman aşımı → 2. denemede gönderildi") ve alıcı bazında **Alıcılar** tablosu (durum ve kanal filtresi, "Gitmeyenler" = başarısız + belirsiz + izin yok, sayfa başına 50). Üst bantlar: kırmızı = key yok, turuncu = MOCK MOD.

## Akış

| Adım | Ne olur | Ne garanti edilir |
|---|---|---|
| 1. Kitle | Claude brief'ten JSON segment kuralı üretir; arayüz kural satırlarını, açıklamayı, kanal bazında kişi sayılarını ve uyarıları gösterir. Doğal dille düzeltme ("Ankara'yı da ekle") eski/yeni kuralı ve sayı farkını yan yana gösterir. | Kural şema + semantik doğrulamadan geçer, parametreli SQL'e derlenir; LLM çıktısı SQL olarak çalışmaz. Brief'te karşılığı olmayan ifadeler `unmapped` uyarısı olarak görünür. Kitlenin tamamı eşlenemiyorsa ("VIP müşterilere") kural uydurulmaz: boş bırakılır, arayüz koşul eklemeyi ister ve kitle boşken içerik adımı ile onay kapalıdır. 0 kişilik kitle onaylanamaz; kitle tüm kullanıcıların %50'sini aşarsa ek onay kutusu zorunludur. |
| 2. İçerik | SMS ve email (konu, preheader, gövde) SSE ile alan alan akar. Kanal bazında yeniden üretim var. Editörde `{{first_name}}` / `{{city}}` "Ad" / "Şehir" etiketleri olarak görünür ve imleç konumuna eklenebilir; saklanan metin değişmez. Önizleme telefon (SMS) ve gelen kutusu (email) görünümündedir. | SMS ≤ 160, konu ≤ 80 karakter; uzunluk kitledeki **en uzun** isim/şehirle ve **boş** isimle render edilerek ölçülür. Yalnızca `{{first_name}}`, `{{city}}` değişkenleri kabul edilir; değişkene yapışık Türkçe ek (`{{city}}'de`) reddedilir. GSM-7 dışı karakterde UCS-2 parça sayısı uyarısı gösterilir. |
| 3. Onay | Pazarlamacı önizler (gerçek bir alıcıyla ve isimsiz render), gerekirse düzenler, kanal kapatabilir, onaylar. | Onay anında kural ve içerik backend'de yeniden doğrulanır. Tek transaction'da yalnızca **o kanalda izni olan** kullanıcılar için mesaj satırları yazılır, içerik alıcı başına render edilip saklanır. `UNIQUE (campaign_id, user_id, channel)` sayesinde çift tıklama çift mesaj üretmez. |
| 4. Gönderim | Worker'lar kuyruktan iş alır, mock provider'a gönderir, hataları sınıflandırıp yeniden dener. | Kanal başına ≤ 100 msg/sn (birden çok worker olsa bile). Aynı kişiye aynı mesaj iki kez gitmez (idempotency key). İzin göndermeden hemen önce tekrar okunur. |
| 5. Canlı izleme | Dashboard ≈2 sn'de bir durum endpoint'ini sorgular: kanal bazında ilerleme, durum dağılımı, saniyelik hız, gönderim günlüğü (yeniden denemeler ve hatalar). | Retry'dan sonra gönderilen mesajlar da hata geçmişiyle ve güncel durumlarıyla (`sent`) "Gönderim günlüğü"nde görünür; `unknown` mesajlar ayrıca açıklanır. |

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
2. **Backend doğrulaması.** Çıktı bilinmeyen alanları reddeden katı JSON decode'dan, ardından semantik doğrulamadan geçer: bilinmeyen alan/operatör, aralık dışı gün sayısı, bilinmeyen şehir (Türkçe normalizasyonla: `istanbul` → `İstanbul`), çelişen koşullar, boş koşul listesi (yalnızca `audience: "all"` ya da eşlenemeyen kitle taslağıyla kabul). Model çıktısı ek olarak herkesi seçen yer tutucu koşulları (`order_count gte 0`) ve eşlenemeyen kitlenin `audience: "all"` ile genişletilmesini reddeder. İçerikte en kötü durum uzunluğu, izin listesi dışı/bozuk değişkenler (`{{phone}}`, `{{first_name`) ve değişkene yapışık ekler (`{{city}}'daki`: değer bilinmediği için ünlü uyumu tutmaz) kontrol edilir. Model içeriği ayrıca brief'te geçmeyen vaatler için taranır (stok, kargo/teslimat, garanti, iade, hediye, süre sınırı); bulunursa onarıma döner ([DECISIONS.md #24](DECISIONS.md)).
3. **Onarım döngüsü.** Doğrulama hataları kod + alan + mesaj listesi olarak ("SMS en kötü durumda 174 karakter, 160'a indir") Claude'a geri gönderilir; en fazla 2 onarım. Sonra hata kullanıcıya gösterilir ve form ile elle düzenleme mümkündür. Refusal, `max_tokens` ile kesilmiş çıktı ve transport hataları onarılmaya çalışılmaz; loglanıp hata olarak döner.
4. **Prompt injection.** Brief, düzeltme talimatı ve mevcut kural etiketli bloklar içinde "güvenilmeyen veri" olarak verilir. Asıl garanti promptta değil doğrulamada: brief ne derse desin çalışan şey yalnızca izin listesindeki koşullardır (`DROP TABLE` içeren alan/değerler testlerde reddediliyor).
5. **Sessiz hata yok.** Brief'in kitleye karşılık gelmeyen kısımları (`"VIP"`, `"genç kullanıcılar"`) `unmapped` olarak döner ve arayüzde uyarı olur. Kitlenin tamamı eşlenemezse kural boş kalır (kimseyle eşleşmez) ve pazarlamacı koşulu elle ekler; çok geniş kitle ek onay ister.
6. **Akış ve kayıt.** İçerik stream edilirken kısmi JSON ayrıştırılıp alanlar arayüzde dolar; tam çıktı stream sonunda doğrulanır ve yalnızca geçerliyse kaydedilir. Onarım turları SSE'de `repair` olayıyla görünür. Tüm çağrılar `llm_calls` tablosunda ve `/metrics`'te (çağrı, onarım, gecikme).
7. **Test edilebilirlik.** `LLM` interface'inin deterministik mock implementasyonu ve senaryolu (scripted) test istemcisi var; onarım döngüsü, vazgeçme, refusal ve stream kesintisi gerçek API key olmadan test edilir. Gerçek API için bir duman testi (`-tags=live`) ve 15 brief'lik bir eval seti (`make eval`) var.

### Model seçimi ve eval

`make eval` 15 örnek brief'i gerçek Claude ile çalıştırır. Her brief'in beklenen kuralı tanımlıdır: koşullar birebir, `audience` ve `unmapped` beklentisiyle karşılaştırılır. Kitlesi olan her brief için içerik de üretilir; doğrulamadan ve brief'te olmayan vaat kontrolünden geçmesi gerekir. Set bilinçli olarak zor durumlar içerir: eşlenemeyen kitle ("VIP"), kısmen eşlenen kitle ("sadık müşterilerden İstanbul'dakiler"), "herkese", şehir hariç tutma, "hiç sipariş vermemiş" ve prompt injection (`DROP TABLE users`). Sonuçlar (2026-11-01):

| Model (segment + içerik) | Doğru kural | İlk denemede | Geçerli içerik | Onarımsız içerik | Toplam süre |
|---|---|---|---|---|---|
| **`claude-sonnet-5-5`** (varsayılan) | **15/15** | 15 | 14/14 | 14 | 77 sn |
| `claude-haiku-5-5` | 12/15 | 12 | 14/14 | 14 | 95 sn |

Haiku'nun üç hatası da aynı türde: koşullar doğru, ama mesaj içeriğine ait ifadeleri ("memnuniyet anketi", "%30 indirim ve samimi ton") `unmapped`'e yazıyor; arayüzde bu, olmayan bir sorun için uyarı demek. Haiku bu iş yükünde daha hızlı da değil. Varsayılan bu yüzden Sonnet 5.5; model `ANTHROPIC_MODEL_SEGMENT` / `ANTHROPIC_MODEL_CONTENT` ile değiştirilebilir ([DECISIONS.md #15](DECISIONS.md)).

Vaat kontrolü eklenmeden önceki prompt'la aynı set 12/14 geçerli içerik verdi: sepet hatırlatmasına brief'te olmayan "stoklar tükenmeden", "hızlı teslimat", "sınırlı" ifadelerini, yılbaşı mesajına "hediye" ekledi. Prompt kuralı bunu azalttı ama sıfırlamadı (bir koşuda "son gün" çıktı); doğrulama + onarım ile 14/14.

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
| `queue.TestExpiredLock*`, `TestUncertain*`, `TestNotSentExhaustedIsFailed` | Çökme kurtarma (veritabanında simüle; gerçek kill için `make chaos`) ve hata sınıflandırması: idempotent modda aynı key ile tekrar, değilse `unknown`; kesin gitmeyen hata tükenirse `failed`. |
| `queue.TestStaleLeaseMarkIsNoop`, `TestRevokedConsentSkipped` | Kira çitleme; izin onaydan sonra geri çekilirse mesaj gitmez. |
| `segment.TestAbandonedCartSemantics` | "20 gün önce alıp 5 gün önce sepete ekleyen" kişi dahil `abandoned_cart` tanımı. |
| `campaign.TestApproveConcurrentOnce` | Eşzamanlı onay istekleri tek set mesaj üretir. |
| `campaign.TestApproveSmallPoolNoDeadlock` | 4 bağlantılık havuzda onay patlaması kilitlenmeden biter (onay tek bağlantı kullanır). |
| `segment.TestValidateRejectsUnknownFieldAndRawSQL`, `segment.TestCompileUsesParamsOnly` | Prompt injection/SQL denemeleri doğrulamada reddedilir; derlenen SQL'de değer yoktur, hepsi parametredir. |
| `llm.TestSegment*`, `TestContentRepairsTooLongSMS`, `TestRefusalNotRepaired` | Onarım döngüsü: bir turda düzelir, 2 turdan sonra vazgeçer, refusal onarılmaz. |
| `api.TestFlow`, `api.TestNoKeyReturns503` | HTTP üzerinden uçtan uca akış; key yokken 503. |
| `campaign.TestStatusKeepsErrorOfRetriedThenSentMessage` | Retry sonrası gönderilen mesaj hata geçmişiyle dashboard'da kalır. |
| `segment.TestValidateIncompleteRule`, `TestValidateGenerated` | Eşlenemeyen kitle boş taslak olarak kalır ve kimseyle eşleşmez; modelin herkesi seçen yer tutucu koşulları reddedilir. |
| `content.TestValidatePlaceholderSuffix` | `{{city}}'daki` gibi yapışık ekler reddedilir. |
| `content.TestUnrequestedClaims`, `llm.TestContentRepairsUnrequestedClaim` | Brief'te olmayan vaat ("stoklar tükenmeden") model çıktısında onarıma döner; brief izin veriyorsa ("kargo bedava" → "ücretsiz kargo") geçer. |
| Frontend (Vitest, 98 test) | GSM-7/UCS-2 parça hesabı, kısmi JSON ayrıştırma, SSE okuyucu, kural yardımcıları, değişken etiketi ↔ metin dönüşümü, gönderim günlüğü biçimlendirme, kampanya adı türetme, önizleme seçimi, durum etiketleri ve filtreler. |

Gerçek Claude ile (key `.env`'de olmalı, `make test`'e dahil değil):

```bash
make eval     # 15 brief'lik eval seti; kaç kuralın doğru, kaç içeriğin geçerli olduğunu tablo halinde yazar (~1-2 dk, birkaç sent)
EVAL_CONTENT=0 make eval   # yalnızca kurallar
cd backend && ANTHROPIC_API_KEY=... go test ./internal/llm -run TestAnthropicSmoke -tags=live -v   # tek brief duman testi
```

### Gerçek çökme testi: `make chaos`

Çalışan stack'e karşı (`make up`) gerçek bir worker'ı gönderimin ortasında `SIGKILL` ile öldürür:

1. LLM'siz bir kampanya oluşturur (herkese, SMS + e-posta, ~8.300 mesaj) ve onaylar.
2. ~400 mesaj gittikten sonra SMS kanalının lider worker'ını `docker kill -s KILL` ile öldürür. Düzgün kapanış yoktur; elindeki istekler yarıda kesilir.
3. O an worker'ın elinde `sending` durumunda kalan mesajları ve bunlardan provider'a ulaşmış ama sonucu kaydedilememiş olanları not eder.
4. Hepsi sonuçlanana kadar bekler, sonra öldürülen worker'ı tekrar başlatır.
5. Kontroller: takılı mesaj yok, kalan worker liderliği devraldı, alıcı başına en fazla 1 teslimat, provider teslim sayısı veritabanındaki `sent` sayısına eşit, yarım kalanların hepsi sonuçlandı ve provider'a ulaşmış olanlar ikinci kez teslim edilmedi.

Sonuçlar (2026-11-01, iki worker):

| Mod | Yarım kalan | Provider'a ulaşmış | Sonuç |
|---|---|---|---|
| Idempotent provider (varsayılan) | 19 | 2 | 19'u `sent`; ulaşmış 2'si provider'da tek teslimat (aynı Idempotency-Key ile tekrar → `duplicate`). 8.278 `sent` = 8.278 teslimat. |
| Idempotency yok (`PROVIDER_IDEMPOTENCY=false MOCK_IDEMPOTENCY=false docker compose up -d worker mock-provider`) | 33 | 10 | 33'ü de `unknown`, hiçbiri tekrar gönderilmedi; ulaşmış 10'u tek teslimat. |

Kampanya listesine "Chaos test" adlı bir kampanya ekler. Kira süresi 30 sn olduğu için yarım kalan mesajlar ~30 sn sonra kurtarılır.

CI: `.github/workflows/ci.yml` (Postgres service container, `go test ./... -race`, frontend test + build).

## Gözlemlenebilirlik

- `GET /healthz` (liveness) ve `GET /readyz` (DB erişimi; `llm: ok|mock|not_configured` bilgisi) — api'de. Worker'larda `/healthz` DB'ye ping atar.
- `GET /metrics` (Prometheus) hem api'de (LLM çağrı sayısı/gecikmesi/doğrulama hatası/onarım) hem her worker'da (`composer_messages_total{channel,outcome}`, provider gecikme histogramı, 429 sayacı, limiter'ın o anki izinli hızı `composer_send_rate`; seriler yalnızca o kanalın lideri olan replikada dolar).
- `GET /api/config`: `{llm_provider, llm_configured, models}` — arayüzdeki bantlar buradan.
- `GET /api/campaigns/{id}/status`: durum başına sayılar, kanal kırılımı, 10 sn'lik hız, son 50 hatalı mesaj.
- `GET /api/campaigns/{id}/messages?status=failed,unknown&channel=sms&page=2`: alıcı bazında mesaj listesi (ad, kanal, durum, deneme, son hata, gönderim zamanı), sayfa başına 50 + toplam.
- `POST /api/segments/members` (`{rule, page, search}`): kuralın kitlesindeki kişiler, sayfa başına 50 + toplam; kural preview ile aynı şekilde doğrulanıp parametreli SQL'e derlenir.
- `PUT /api/campaigns/{id}/name` (`{name}`): kampanya adını değiştirir (boş olamaz, en fazla 120 karakter; onaydan sonra da).
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
- **Eval setinin genişliği:** `make eval` 15 brief ile sınırlı ve içerik kalitesini (ton, ikna gücü) puanlamaz; yalnızca geçerliliği ölçer. Metin kalitesi karşılaştırması elle yapıldı.
- **Türkçe ek çekimi:** değişkene ek yapıştırmak yasak; ekin değere göre otomatik çekilmesi (İstanbul'da / İzmir'de) yapılmadı.

## Bilinen sınırlamalar

- **`abandoned_cart` sadeleştirmesi:** son sepete eklemeden sonra alakasız bir ürün alınmışsa kişi kitleden çıkar; ürün seviyesinde sepet takibi yok.
- **Mock provider'ın idempotency kayıtları bellekte:** provider container'ı yeniden başlarsa sıfırlanır.
- **Efektif tavan ≈95 msg/sn/kanal:** limiter 50 ms güvenlik payı tutar ki provider tarafındaki her 1 sn penceresinde 100 aşılmasın.
- **Liderlik devrinde kısa çakışma:** lider kilit bağlantısını kaybederse, eski liderin uçuştaki (en fazla eşzamanlılık kadar, her biri 5 sn timeout'lu) istekleri yeni liderin ilk istekleriyle çakışabilir; provider'ın 429'u yedek koruma.
- **İzin iş alma anında okunur:** izin, mesajı kuyruktan alan sorguda okunur; normalde gönderimden 1 sn'den kısa süre önce, provider'ın Retry-After duraklaması sırasında ise ~10 sn'ye kadar önce. Bu aralıkta izin geri çekilirse o mesaj yine gider.
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
