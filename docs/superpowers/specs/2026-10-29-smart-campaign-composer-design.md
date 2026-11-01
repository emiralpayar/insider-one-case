# Smart Campaign Composer — Tasarım Dokümanı

Tarih: 2026-10-29 · Durum: onaylandı (kullanıcı ile tartışılarak netleştirildi)

Kaynak gereksinimler: `docs/brief.md` (Insider One case metni, 7 gün teslim). Bu doküman onu kapsar; çelişki olursa brief geçerlidir.

## 1. Amaç

Pazarlamacı doğal dille bir kampanya isteği yazar ("Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun."). Sistem:

1. Claude ile brief'ten **doğrulanmış bir segment kuralı** çıkarır, kitle büyüklüğünü gösterir.
2. SMS ve email içeriğini **stream ederek** üretir, kanal limitlerine göre doğrular.
3. İnsan önizler, düzenler, **onaylar**.
4. Mesajlar **Postgres tabanlı kuyruk** üzerinden mock provider'a gönderilir.
5. Dashboard gönderim durumunu **canlı** gösterir.

Değerlendirmede en ağır kalem LLM entegrasyonu (%25): Claude çıktısının güvenilir hale getirilmesi (yapılandırılmış JSON, backend doğrulaması, onarım döngüsü, mock LLM ile test) bu projenin merkezidir.

## 2. Hedefler / Hedef olmayanlar / Anti-hedefler

**Hedefler**
- Çekirdek akış uçtan uca sağlam: brief → segment → içerik → onay → gönderim → canlı izleme.
- Aynı kişiye aynı mesaj iki kez gitmez (çift gönderim, hiç gitmemesinden daha kötü kabul edildi).
- Kanal başına saniyede en fazla 100 mesaj, birden fazla worker olsa bile.
- İzni olmayan kişiye o kanaldan mesaj gitmez; izin onaydan sonra geri çekilirse de gitmez.
- Testler gerçek API key olmadan (mock LLM) çalışır.
- `docker compose up` tek komut, `make test` çalışır.

**Hedef olmayanlar (README'de belirtilecek)**
- Kimlik doğrulama / çoklu kiracı (multi-tenant).
- Gerçek SMS/email provider entegrasyonu.
- A/B varyantları (yalnızca "yeniden üret" var; A/B sonraki adım).
- Redis/Kafka gibi ek altyapı.

**Anti-hedefler (teknik olarak çalışsa bile başarısızlık sayılır)**
- LLM çıktısının herhangi bir yolla SQL olarak çalıştırılması.
- Pazarlamacının fark etmeden yanlış/çok geniş kitleye gönderim yapabilmesi (sessizce düşen brief parçaları, "herkese" gönderim).
- Çökme/timeout sonrası aynı kişiye ikinci SMS.

## 3. Teknoloji

| Katman | Seçim |
|---|---|
| Backend | Go (tek binary, `api` ve `worker` alt komutları) |
| Frontend | Vue 3 + TypeScript + Vite |
| DB + kuyruk | PostgreSQL 16 (`FOR UPDATE SKIP LOCKED`, advisory lock) |
| LLM | Claude API, resmi Go SDK (`anthropic-sdk-go`); amaç başına model: `ANTHROPIC_MODEL_SEGMENT` (varsayılan `claude-sonnet-5-5`), `ANTHROPIC_MODEL_CONTENT` (varsayılan `claude-sonnet-5-5`; Haiku 5.5 ölçülerek değerlendirilecek) |
| Mock provider | Ayrı Go servisi (ayrı container) |
| Canlı durum | Frontend birkaç saniyede bir (≈2 sn) durum endpoint'ini sorgular (polling) |
| İçerik stream'i | SSE (backend → frontend) |

## 4. Bileşenler (docker compose)

```
web (nginx + Vue build)  ──►  api (Go)  ──►  postgres
                                  ▲              ▲
                                  │              │
                           worker (Go, N adet) ──┘──► mock-provider (Go)
                                  │
                                  └── Claude API (veya mock LLM)
```

- **postgres**: tüm durum + kuyruk.
- **api**: HTTP API, migration'ları açılışta çalıştırır, DB boşsa seed eder.
- **worker**: kuyruğu işler; compose'ta 2 replika (çoklu worker ve rate limit davranışı gösterilebilsin diye).
- **mock-provider**: SMS/email provider taklidi.
- **web**: Vue SPA, `/api` isteklerini api'ye proxy eder.

## 5. Veri modeli (özet)

- `users`: id, first_name (boş olabilir), last_name, email, phone, city, language (`tr`/`en`), created_at, `sms_consent`, `email_consent`, consent_updated_at.
- `events`: id, user_id, type (`cart_add` | `purchase`), occurred_at, amount (purchase için). Davranışsal kurallar buradan sorgulanır.
- `campaigns`: id, brief, llm_provider (`anthropic`|`mock`), channels (varsayılan `{sms,email}`; pazarlamacı onaydan önce bir kanalı kapatabilir), status (`draft` → `audience_ready` → `content_ready` → `approved`/`sending` → `completed`), segment_rule (JSONB), content (JSONB), approved_at, created_at.
- `messages` (kuyruk): id (UUID, aynı zamanda idempotency key), campaign_id, user_id, channel (`sms`|`email`), rendered içerik (onay anında render edilip saklanır), status (`queued`|`sending`|`sent`|`failed`|`unknown`|`skipped_no_consent`), attempts, next_attempt_at, locked_until, last_error, provider_message_id, sent_at. **`UNIQUE (campaign_id, user_id, channel)`**.
- `llm_calls`: her Claude çağrısının kaydı — amaç (segment/refine/content), girdi, ham çıktı, doğrulama hataları, deneme no, gecikme, token kullanımı. Hata ayıklama ve review için.

Seed: ~5000 deterministik sahte kullanıcı (Türkçe isimler, ~%5'inin first_name'i boş, Türkiye şehirleri, karışık izinler, son 120 güne yayılmış cart_add/purchase event'leri; sepet terk eden, alıp tekrar ekleyen vb. senaryolar bilinçli olarak üretilir).

## 6. Segment kuralı

### 6.1 DSL

Claude SQL üretmez; yalnızca aşağıdaki şemaya uyan JSON üretir (structured output ile şemaya zorlanır):

```json
{
  "audience": "filtered",
  "conditions": [
    {"field": "city", "op": "in", "value": ["İstanbul"]},
    {"field": "abandoned_cart", "op": "within_days", "value": 30}
  ],
  "unmapped": [],
  "explanation": "Son 30 günde sepete ürün ekleyip sonrasında alışveriş yapmamış İstanbullu kullanıcılar"
}
```

- Koşullar her zaman **AND** ile birleşir. Şehir gibi alanlarda "veya" ihtiyacı `in` operatörüyle karşılanır.
- `audience: "all"` yalnızca kullanıcı açıkça herkesi istediğinde; bu durumda `conditions` boş olur. `audience: "filtered"` + boş `conditions` **geçersizdir**.
- `unmapped`: brief'te karşılığı olmayan istekler (ör. "VIP müşteriler"). Sessizce atlanmaz, arayüzde uyarı olarak gösterilir.
- `explanation`: Claude'un brief'i nasıl anladığı, insan okuyacak şekilde.

### 6.2 Alan izin listesi (whitelist)

| field | op | value |
|---|---|---|
| `city` | `in`, `not_in` | bilinen şehir listesinden string dizisi |
| `language` | `eq` | `tr` \| `en` |
| `abandoned_cart` | `within_days` | 1–365 |
| `purchased` | `within_days`, `not_within_days` | 1–365 |
| `order_count` | `gte`, `lte` | 0–1000 |
| `total_spent` | `gte`, `lte` | 0–1.000.000 |
| `signed_up` | `within_days` | 1–3650 |

**Anlamı hazır koşullar**: `abandoned_cart within_days N` = "son N günde en az bir `cart_add` var **ve** en son `cart_add`'den sonra hiç `purchase` yok". Örnek: 20 gün önce alışveriş yapmış, 5 gün önce sepete ekleyip almamış kişi kitleye **girer**. Bu tanım bir kez backend'de yazılır ve test edilir; Claude'un ilkel parçaları yanlış birleştirmesine izin verilmez. (Bilinen sadeleştirme: son sepete eklemeden sonra *alakasız* bir ürün alınmışsa kişi kitleden çıkar; ürün seviyesinde sepet takibi kapsam dışı.)

### 6.3 Doğrulama ve derleme

1. JSON şema doğrulaması (structured output'a rağmen backend tekrar doğrular).
2. Semantik doğrulama: alan izin listesinde mi, operatör bu alana uygun mu, değer tipi ve aralığı doğru mu, şehir bilinen listede mi, aynı alanda çelişkili koşul var mı.
3. Geçerli kural **parametreli SQL**'e derlenir (query builder; değerler her zaman `$n` parametresi). LLM'den gelen hiçbir string SQL metnine girmez.

### 6.4 Onarım döngüsü

Doğrulama hatası olursa hatalar makine-okunur listesiyle Claude'a geri gönderilir ("`city` değeri `Istanbull` bilinen şehirlerden değil", vb.). En fazla **2 onarım denemesi**; yine geçersizse arayüzde anlaşılır hata + pazarlamacı kuralı formla elle kurabilir.

### 6.5 Düzenleme

- **Form ile**: her koşul bir satır (alan / operatör / değer). Değişiklik sonrası `/segments/preview` çağrılır (debounce'lu).
- **Doğal dille**: "Ankara'yı da ekle". Mevcut kural + talimat Claude'a gider, yeni kural aynı doğrulamadan geçer. Arayüzde eski↔yeni fark ve kişi sayısı değişimi gösterilir; pazarlamacı kabul eder ya da reddeder.

### 6.6 Kitle önizleme ve uyarılar

Önizleme döner: toplam eşleşen kişi, SMS izni olan, email izni olan, ve birkaç örnek kullanıcı.

Uyarılar (onay ekranında da tekrar edilir):
- Kitle toplam kullanıcıların **%50'sinden fazla** → belirgin uyarı + onayda ayrıca "geniş kitleyi onaylıyorum" kutusu zorunlu.
- Kitle **0** → onay butonu kapalı.
- `unmapped` boş değil → sarı uyarı.

## 7. İçerik üretimi

### 7.1 Çıktı yapısı

```json
{
  "sms": "Merhaba {{first_name}}, sepetindekiler seni bekliyor! %30 indirim için: ...",
  "email": {"subject": "...", "preheader": "...", "body": "..."}
}
```

Email body sade metin/minimal HTML. **Dil: tüm alıcılara Türkçe** (kullanıcı diline göre içerik bonus kapsamında, çekirdekte yok; README'de belirtilir). Kitle `en` dilli kullanıcı içeriyorsa önizlemede bilgi notu gösterilir.

### 7.2 Streaming

İçerik Claude'dan stream edilir; backend gelen kısmi JSON'u SSE ile frontend'e aktarır, frontend alanları geldikçe doldurur. Stream bittiğinde tam JSON backend'de doğrulanır; frontend son halini doğrulama sonucuyla birlikte alır.

### 7.3 Değişkenler

- İzin listesi: `{{first_name}}`, `{{city}}`. Bilinmeyen değişken (ör. `{{phone}}`) → doğrulama hatası.
- **Boş isim**: varsayılan değer *yoktur* (ör. "Merhaba" varsayılanı "Merhaba Merhaba" üretirdi). Değişken boş string olarak render edilir ve render sonrası boşluk/noktalama temizlenir: `"Merhaba {{first_name}}, ..."` → `"Merhaba, ..."`. Prompt, Claude'dan cümlelerin isim olmadan da doğal okunacak şekilde kurulmasını ister. Doğrulama hem en uzun isimle hem boş isimle render edip ikisini de kontrol eder.

### 7.4 Limitler

- **SMS ≤ 160 karakter**, **email konusu ≤ 80 karakter** (brief'teki tanım; karakter = Unicode rune).
- Uzunluk **en kötü durumla** ölçülür: değişkenler, o kampanyanın kitlesindeki en uzun değerle (en uzun first_name, en uzun city) render edilip sayılır.
- **Türkçe karakter uyarısı**: SMS GSM-7 dışı karakter (ş, ğ, ı, İ vb.) içeriyorsa limit yine 160 kalır ama arayüzde "Unicode (UCS-2) olarak gider, N SMS parçası olarak faturalanır" uyarısı gösterilir (UCS-2: tek parça 70, çok parçada parça başına 67 karakter). Gerekçe DECISIONS.md'de.

### 7.5 Onarım ve yeniden üretme

- Limit aşımı / bilinmeyen değişken → Claude'a somut hata ile onarım isteği ("SMS en kötü durumda 174 karakter, 160'a indir"). En fazla 2 deneme.
- **Yeniden üret**: kanal bazında (yalnızca SMS veya yalnızca email).
- **İnsan düzenlemesi**: canlı karakter sayacı + GSM-7 uyarısı frontend'de; onay anında backend aynı doğrulamayı tekrar yapar.

## 8. Claude entegrasyonu

- `LLM` interface'i: `GenerateSegment`, `RefineSegment`, `StreamContent`. İki implementasyon:
  - `anthropic`: resmi Go SDK; structured output (`output_config.format` JSON schema) ile şemaya uygun çıktı; adaptive thinking, düşük/orta effort; refusal stop reason'u ele alınır.
  - `mock`: deterministik, anahtar kelime tabanlı fixture'lar; testler ve key'siz geliştirme için. Hatalı çıktı senaryoları da üretebilir (onarım döngüsünü test etmek için).
- Seçim: varsayılan `anthropic`. **Key yoksa sessizce mock'a düşülmez**: API açılır ama LLM endpoint'leri `503 llm_not_configured` döner, `/readyz` `llm: not_configured` raporlar, frontend kırmızı bant gösterir. Mock yalnızca testlerde ve açıkça `LLM_PROVIDER=mock` verildiğinde çalışır; bu durumda frontend kalıcı "MOCK MOD" bandı gösterir ve kampanyalar `llm_provider='mock'` olarak işaretlenir (dashboard'da görünür). `GET /api/config` → `{llm_provider, llm_configured, models}`.
- Brief güvenilmeyen girdi kabul edilir (prompt injection): çıktı her durumda şema + semantik doğrulamadan geçer, yani brief ne derse desin çalışan şey yalnızca izin listesindeki koşullardır.
- Tüm çağrılar `llm_calls` tablosuna yazılır.
- Golden test seti: örnek brief'ler → beklenen kurallar (mock ile `make test`'te; `make eval` gerçek API ile — uygulandı, sonuçlar README'de).

## 9. Onay ve kuyruğa alma

`POST /campaigns/{id}/approve`, **tek transaction** içinde:
1. Kural ve içeriği son kez doğrular; büyük kitle ise onay bayrağını kontrol eder.
2. Kitleyi sorgular; her kanal için yalnızca o kanalda izni olan kullanıcılara `messages` satırı yazar (içerik bu anda render edilip saklanır).
3. `ON CONFLICT (campaign_id, user_id, channel) DO NOTHING` — çift onay/çift tıklama çift satır üretmez.
4. Kampanya `sending` olur.

## 10. Gönderim (worker)

### 10.1 İş alma
```sql
UPDATE messages SET status='sending', locked_until=now()+'30s', attempts=attempts+1
WHERE id IN (
  SELECT id FROM messages
  WHERE channel=$1 AND (status='queued' OR (status='sending' AND locked_until<now()))
    AND next_attempt_at<=now()
  ORDER BY next_attempt_at LIMIT $2 FOR UPDATE SKIP LOCKED)
RETURNING ...
```
Kilit süresi dolan `sending` satırları başka worker'a geçer (çökme kurtarma).

### 10.2 Rate limit (çoklu worker)
- Kanal başına **tek gönderici**: worker `pg_try_advisory_lock(<kanal>)` ile kanalın liderliğini alır. Kilidi alan worker o kanalın hız sınırlayıcısını (100/sn, eşit aralıklı; uygulamada token bucket yerine pacing seçildi, bkz. DECISIONS #2) tutar, gönderimleri goroutine havuzuyla paralel yapar.
- Lider çökerse DB bağlantısı kopar, kilit otomatik düşer, diğer worker devralır.
- Böylece limit tek yerde sayılır; dağıtık sayaç gerekmez.

### 10.3 Gönderimden hemen önce
- İzin tekrar okunur; geri çekilmişse gönderilmez → `skipped_no_consent`.

### 10.4 Hata sınıflandırması ve retry

| Sonuç | SMS gitti mi? | Aksiyon |
|---|---|---|
| 2xx | evet | `sent` |
| 2xx "duplicate" (idempotency key daha önce görülmüş) | evet (önceden) | `sent` |
| 429 | hayır | `next_attempt_at = now + Retry-After` (yoksa backoff); **deneme hakkından düşmez**; kanalın hız sınırlayıcısı `Retry-After` kadar durdurulur ve hız geçici olarak düşürülüp kademeli artırılır |
| 4xx (429 hariç) | hayır | `failed` (kalıcı hata, retry yok) |
| bağlantı kurulamadı | hayır | backoff ile retry |
| 5xx / timeout / istek sırasında çökme | **belirsiz** | provider idempotency destekliyorsa aynı key ile retry; desteklemiyorsa `unknown`, retry yok |

- Backoff: üstel (1s, 2s, 4s… tavan 60s) + jitter. Maksimum 5 deneme. Deneme hakkı biterse: son hata kesin-gitmedi türündeyse `failed`, belirsiz türdeyse `unknown` (gitmiş olabilir; `failed` demek yanıltıcı olur).
- Kilit süresi dolmuş `sending` satırı (worker istek sırasında çöktü) belirsiz sayılır: idempotency destekleyen provider'da aynı key ile tekrar denenir, desteklemeyende `unknown` yapılır, tekrar gönderilmez.
- Her istek `Idempotency-Key: <message_id>` başlığı taşır.
- Provider'ın idempotency desteği bir provider yeteneği olarak modellenir (config); mock provider destekler. Desteklemeyen provider senaryosu (`unknown`, retry yok) kodda da var ve test edilir.

### 10.5 Kampanya tamamlanması
Tüm `messages` satırları terminal durumdaysa (`sent`/`failed`/`unknown`/`skipped_no_consent`) kampanya `completed`.

## 11. Mock provider

- `POST /sms`, `POST /email`: `Idempotency-Key` zorunlu. Aynı key ikinci kez gelirse yeniden "göndermez", ilk sonucu döner.
- Env ile ayarlanabilir: `FAIL_RATE` (5xx), `RATE_429` (rastgele 429), `LATENCY_MS_MIN/MAX`, `TIMEOUT_RATE` (cevap vermeden bekleme), kendi rate limiti (kanal başı 100/sn aşılırsa 429 + `Retry-After`).
- **"Gönderildi ama cevap kayboldu" senaryosu**: belirli oranda mesajı kaydeder ama 5xx/timeout döner — idempotency'nin gerçekten çift gönderimi engellediğini göstermek için.
- `GET /messages/{id}`: provider tarafındaki durum.
- `GET /stats`: alıcı başına gönderim sayısı — "aynı kişiye iki mesaj gitti mi" testi bunu kullanır.
- İdempotency kayıtları bellekte tutulur (provider yeniden başlarsa sıfırlanır; mock için kabul).

## 12. Takip endpoint'leri

- `GET /api/campaigns/{id}/status`: durum başına sayılar, kanal kırılımı, son hatalar, saniyelik gönderim hızı. Frontend ≈2 sn'de bir sorgular.
- `GET /healthz` (liveness), `GET /readyz` (DB erişimi).
- `GET /metrics` (Prometheus formatı): gönderilen/başarısız/retry sayaçları, provider gecikmesi histogramı, LLM çağrı sayısı/gecikmesi/doğrulama hatası/onarım sayısı, kuyruk derinliği.

## 13. API (taslak)

| Method | Path | Amaç |
|---|---|---|
| POST | `/api/campaigns` | brief ile taslak oluştur |
| GET | `/api/campaigns`, `/api/campaigns/{id}` | listele / detay |
| POST | `/api/campaigns/{id}/segment/generate` | Claude ile kural üret |
| POST | `/api/campaigns/{id}/segment/refine` | doğal dille düzelt (öneri döner, kaydetmez) |
| PUT | `/api/campaigns/{id}/segment` | kuralı doğrula + kaydet |
| POST | `/api/segments/preview` | kural için kişi sayıları + uyarılar |
| GET | `/api/campaigns/{id}/content/stream?channel=sms\|email\|all` | SSE ile içerik üret |
| PUT | `/api/campaigns/{id}/content` | düzenlenmiş içeriği doğrula + kaydet |
| POST | `/api/campaigns/{id}/approve` | onayla + kuyruğa al |
| GET | `/api/campaigns/{id}/status` | canlı durum |

## 14. Frontend

- **Composer** (adımlı): Brief → Kitle (kural satırları, explanation, kişi sayıları, uyarılar, doğal dille düzeltme kutusu, fark görünümü) → İçerik (stream edilen SMS/email, karakter sayaçları, GSM-7 uyarısı + parça sayısı, örnek kullanıcıyla render önizleme, kanal bazında yeniden üret) → Onay (özet + uyarılar + geniş kitle onay kutusu).
- **Dashboard**: kampanya durumu, kanal bazında ilerleme, durum dağılımı, hız, son hatalar; polling.
- **Kampanya listesi**.

## 15. Test stratejisi (`make test`)

- Go unit: segment validator, SQL derleyici (parametre kullanımı), render + boş isim temizliği, uzunluk/GSM-7 hesabı, hata sınıflandırma, backoff, LLM onarım döngüsü (mock LLM hatalı çıktı verir → onarılır / 2 denemede vazgeçer).
- Go entegrasyon (gerçek Postgres, compose'daki test DB): `abandoned_cart` semantiği (kullanıcının verdiği örnek dahil), onayda dedup, izin geri çekilmesi, SKIP LOCKED ile çoklu worker, lock süresi dolunca devralma, uçtan uca: mock provider hata/429/kayıp-cevap modunda → her alıcıya tam bir mesaj (provider `/stats` ile doğrulanır), rate limit gözlemi.
- Frontend: Vitest ile karakter sayacı / GSM-7 tespiti / render.

## 16. Teslim ve sıralama

Kullanıcı isteği: önce **seed + mock provider**. Uygulama büyük parçalar halinde:
1. İskelet: repo yapısı, compose, migration'lar, seed, mock provider.
2. Kuyruk/worker: iş alma, advisory lock + rate limit, retry/idempotency, status/metrics/health.
3. Segment DSL: validator, derleyici, preview.
4. LLM katmanı: mock + Anthropic, segment/refine, içerik stream'i, onarım, llm_calls.
5. Kampanya API akışı + onay.
6. Frontend.
7. README, DECISIONS.md, CI (GitHub Actions: `make test`), `ai-sessions/`.

## 17. Öncelik (7 günlük bütçe)

**Zorunlu (çekirdek):** seed, mock provider, kuyruk/worker/rate limit/retry/idempotency, izin kontrolleri, segment DSL + doğrulama + derleme + preview, LLM katmanı (mock + Anthropic) ve onarım döngüsü, içerik stream'i ve limit/değişken doğrulaması, onay akışı, composer + dashboard ekranları, health/status, `make test`, README/DECISIONS/ai-sessions.

**Sade tutulacak:** `/metrics` (birkaç temel sayaç/histogram), doğal dille düzeltmede fark görünümü (eski/yeni kural yan yana + sayı farkı), 429 sonrası hız toparlanması (basit sabit adımlı artış).

**Zaman kalırsa (yoksa README'de belirtilir):** `make eval` (gerçek API ile golden set), CI pipeline, zamanlanmış gönderim, dile göre içerik, A/B.

## 18. Builder'a bırakılanlar

Paket yapısı, HTTP router seçimi, migration aracı, CSS/UI kütüphanesi, prompt metinlerinin tam hali, seed dağılım detayları, metrik isimleri.
