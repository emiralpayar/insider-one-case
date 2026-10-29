# DECISIONS

Projedeki önemli kararlar ve gerekçeleri. Ayrıntılı tasarım: `docs/superpowers/specs/2026-10-29-smart-campaign-composer-design.md`.

## 1. Kuyruk olarak Postgres (`SKIP LOCKED`), Redis/Kafka yok

**Karar:** Mesaj kuyruğu Postgres'teki `messages` tablosu. Worker'lar `FOR UPDATE SKIP LOCKED` ile iş alır.

**Neden:**
- "Aynı kişiye aynı mesaj iki kez gitmez" kuralının ilk katmanı `UNIQUE (campaign_id, user_id, channel)` kısıtı. Onay + kuyruğa alma tek transaction'da. Ayrı bir kuyruk sistemi olsaydı "DB'ye yazdım ama kuyruğa push edemedim" (dual-write) sorunu çıkardı ve outbox gerekirdi.
- Ölçek (birkaç bin alıcı, kanal başı 100 msg/sn) Postgres için çok küçük.
- Daha az hareketli parça: tek bağımlılık.

**Bedeli / ne zaman değişir:** Gerçek Insider ölçeğinde (milyonlarca mesaj, çok sayıda instance) kuyruk Kafka/Redis Streams'e taşınmalı. Kuyruk erişimi bir interface arkasında.

## 2. Rate limit: kanal başına tek gönderici (advisory lock)

**Karar:** Her kanal için `pg_try_advisory_lock` ile bir lider worker seçilir. Lider bellekte token bucket (100/sn) tutar ve gönderimleri goroutine'lerle paralel yapar.

**Neden:** Limit tek yerde sayılır, dağıtık sayaç (Redis vb.) gerekmez. Lider çökerse DB oturumu kapanır, kilit otomatik düşer, başka worker devralır.

**Bedeli:** Bir kanalın gönderim kapasitesi tek bir process ile sınırlı. 100/sn için fazlasıyla yeterli. Çok daha yüksek limitlerde dağıtık rate limiter gerekir.

- Limiter 50 ms güvenlik payı tutar (100 gönderim 1,05 sn'ye yayılır), bu yüzden efektif tavan ≈95/sn. Provider istekleri varış zamanına göre saydığı için ağ gecikmesindeki oynamalara rağmen provider tarafındaki her 1 sn penceresinde ≤100 garanti edilir.
- Kilit bağlantısı koparsa liderlik devri sırasında eski liderin uçuştaki gönderimleri (en fazla `Concurrency` adet, her biri 5 sn timeout ile sınırlı) yeni liderin ilk gönderimleriyle kısa süre çakışabilir. Bu çakışma sınırlıdır; provider'ın 429'u yedek koruma sağlar. Lider her claim öncesi kilit bağlantısını kontrol ederek bu pencereyi daraltır.

## 3. Çift gönderim > hiç gitmeme: idempotency key

**Karar:** Çift SMS, hiç gitmemesinden daha kötü kabul edildi.
- Her provider isteği `Idempotency-Key: <message_id>` taşır. Mock provider aynı key'i ikinci kez görürse göndermez, ilk sonucu döner.
- Hatalar "kesin gitmedi" (bağlantı kurulamadı, 429, 4xx) ve "belirsiz" (5xx, timeout, istek sırasında çökme) olarak ayrılır. Kesin gitmeyenler güvenle tekrar denenir.
- Belirsiz durumda: provider idempotency destekliyorsa aynı key ile tekrar denenir. **Desteklemiyorsa mesaj `unknown` işaretlenir ve tekrar denenmez** (at-most-once).

- Deneme hakkı biten belirsiz mesajlar `failed` değil `unknown` olur, çünkü gitmiş olabilirler.

**Neden:** Exactly-once dış bir provider ile mümkün değil. Idempotency key pratikte "bir kez" sağlar. Olmadığı durumda pazarlama mesajı için kaçırılan bir mesaj, mükerrer mesajdan daha az zararlı.

## 4. 429 deneme hakkından düşmez, kanalı yavaşlatır

**Karar:** 429'da mesaj `Retry-After` kadar ertelenir ve attempt sayılmaz. Kanalın token bucket'ı aynı süre durdurulur, hız geçici olarak düşürülüp kademeli artırılır.

**Neden:** 429 mesajla değil kanalın hızıyla ilgili ve mesajın gitmediği kesin. Kanalı yavaşlatmazsak sıradaki mesajlar da 429 alır.

## 5. İzin iki kez kontrol edilir

**Karar:** Onayda yalnızca izni olanlar için mesaj satırı açılır. Worker göndermeden hemen önce izni tekrar okur, geri çekilmişse `skipped_no_consent` yapar.

**Neden:** Onay ile gönderim arasında izin geri çekilebilir. İzinsiz mesaj hukuki risk.

## 6. LLM SQL üretmez: kısıtlı segment DSL'i

**Karar:** Claude yalnızca şemaya uyan bir JSON kural üretir (alan izin listesi, alana uygun operatörler, aralık kontrollü değerler). Backend şema + semantik doğrulama yapar ve kuralı parametreli SQL'e derler.

**Neden:** Brief'te ne yazarsa yazsın (prompt injection dahil) çalışan şey yalnızca izin listesindeki koşullardır.

## 7. Anlamı hazır koşullar (`abandoned_cart`)

**Karar:** "Sepeti terk etti" gibi kavramlar ilkel parçaların (sepete ekledi + alışveriş yapmadı) birleşimi olarak değil, backend'de tanımlı tek bir koşul olarak sunulur. `abandoned_cart within_days N` = son N günde `cart_add` var **ve** en son `cart_add`'den sonra `purchase` yok.

**Neden:** "Son 30 günde sepete ekledi VE son 30 günde alışveriş yok" kuralı, 20 gün önce alıp 5 gün önce sepete ekleyip almayan kişiyi yanlışlıkla dışarıda bırakıyordu (tasarım tartışmasında yakalandı). Doğru tanım bir kez yazılıp test edilir, Claude'un hata yapabileceği alan küçülür.

**Bilinen sadeleştirme:** Son sepete eklemeden sonra alakasız bir ürün alınmışsa kişi kitleden çıkar. Ürün seviyesinde sepet takibi kapsam dışı.

## 8. Eşlenemeyen istekler ve "herkese" gönderim

**Karar:**
- Claude, brief'te karşılığı olmayan kısımları `unmapped` alanında döner. Bunlar arayüzde uyarı olarak gösterilir, sessizce atlanmaz.
- Boş koşul listesi geçersiz, ancak açıkça `audience: "all"` ile kabul edilir.
- Kitle tüm kullanıcıların %50'sinden fazlaysa ek onay kutusu zorunlu. 0 kişilik kitle onaylanamaz.

**Neden:** En tehlikeli LLM hatası, sessizce çok geniş kitle seçmek.

## 9. Doğrulama hatasında otomatik onarım (en fazla 2 deneme)

**Karar:** Şema/semantik/limit hataları, makine-okunur hata listesiyle Claude'a geri gönderilir. 2 başarısız onarımdan sonra kullanıcıya hata gösterilir ve form ile elle düzenleme mümkündür.

**Neden:** Çoğu hata (fazla uzun SMS, yanlış yazılmış şehir) tek turda düzelir. Sınırsız döngü maliyet ve gecikme demek.

## 10. SMS limiti 160 karakter, Türkçe karakter uyarısı

**Karar:** Brief "SMS en fazla 160 karakter" dediği için limit 160 karakter (Unicode rune) olarak uygulanır. SMS GSM-7 alfabesi dışında karakter (ş, ğ, ı, İ vb.) içeriyorsa arayüz uyarı gösterir: mesaj UCS-2 kodlamasıyla gider ve kaç SMS parçası olarak faturalanacağı gösterilir (UCS-2'de tek parça 70, çok parçalı mesajda parça başına 67 karakter).

**Neden:** Gerçek operatörlerde Türkçe karakter içeren 160 karakterlik bir SMS 3 parça olarak faturalanır. Brief'in tanımına uyuyoruz ama maliyet etkisini pazarlamacıdan saklamıyoruz. Gerçek bir üründe limit kodlamaya göre dinamik olmalı ya da Türkçe karakterler GSM-7 karşılıklarına çevrilmeli (ş→s); bu bir ürün kararı.

## 11. Kişiselleştirme: boş isimde varsayılan yok, temizleme var

**Karar:** `{{first_name}}` boşsa boş string olarak render edilir, ardından boşluk/noktalama temizlenir (`"Merhaba {{first_name}}, ..."` → `"Merhaba, ..."`). Uzunluk kontrolü hem kitledeki en uzun değerle hem boş değerle yapılır.

**Neden:** "Merhaba" gibi bir varsayılan "Merhaba Merhaba" üretiyordu. Değişken bağlamdan bağımsız bir varsayılan doğal cümle kuramaz. Prompt, Claude'dan cümleleri isim olmadan da doğal okunacak şekilde kurmasını ister.

## 12. İçerik onay anında render edilip saklanır

**Karar:** Her alıcının mesaj metni onayda render edilip `messages` satırına yazılır.

**Neden:** Gönderilen metin denetlenebilir. Onaydan sonra şablon değişse de gönderilen içerik değişmez. Retry'ler aynı metni gönderir.

## 13. Canlı durum için polling (SSE/LISTEN-NOTIFY değil)

**Karar:** Dashboard durum endpoint'ini ≈2 sn'de bir sorgular. SSE yalnızca içerik üretimi stream'inde kullanılır.

**Neden:** Basit, sağlam, yeniden bağlanma derdi yok. Birkaç saniyelik gecikme bu ekran için yeterli.

## 14. Mock LLM: yalnızca testlerde ve açık seçimle, asla sessizce

**Karar:** `LLM` interface'inin `anthropic` ve `mock` implementasyonları var. Key yoksa sistem mock'a **düşmez**: LLM endpoint'leri `503 llm_not_configured` döner ve arayüzde kırmızı bant görünür. Mock testlerde ve yalnızca `LLM_PROVIDER=mock` açıkça verildiğinde çalışır. Bu durumda arayüzde kalıcı "MOCK MOD" bandı görünür ve kampanya `llm_provider='mock'` olarak işaretlenir. Mock, onarım döngüsünü test etmek için bilinçli olarak hatalı çıktı da üretebilir.

**Neden:** Key unutulursa sessiz fallback, sahte kampanya üretip gerçekmiş gibi gönderilmesine yol açar ve kimse fark etmez. Testler ise gerçek API olmadan deterministik çalışmalı.

## 15. Model seçimi: amaca göre, ölçerek

**Karar:** Model amaca göre ayrı seçilir: `ANTHROPIC_MODEL_SEGMENT` (varsayılan `claude-sonnet-5-5`) ve `ANTHROPIC_MODEL_CONTENT` (varsayılan `claude-sonnet-5-5`). Yapılandırılmış çıktı için structured output (JSON schema) kullanılır.

**Neden:** İki iş de kısa ve iyi tanımlı. Segment çıktısı her durumda backend doğrulamasından geçiyor, içerik limitlerini onarım döngüsü yakalıyor. Opus'un ek gücü bu işlerde maliyet ve gecikmeye değmez. Haiku 5.5 içerik için çok daha ucuz ve hızlı, ama Türkçe "samimi ama kısa" metin kalitesi ölçülmeden varsayılan yapılmadı. Golden brief seti iki modelle çalıştırılıp onarım oranı, gecikme ve metin kalitesi karşılaştırılacak, sonuç buraya yazılacak.

## 16. Kuyruk güncellemeleri kira token'ı ile çitlenir

**Karar:** Claim'in yazdığı `locked_until` değeri bir fencing token olarak kullanılır. Tüm `Mark*` güncellemeleri `where id=$1 and status='sending' and locked_until=$2` koşuluyla çalışır; eşleşme yoksa no-op olur (`ErrLeaseLost`).

**Neden:** Bir worker yavaşlayıp kirası dolarsa satır başka worker'a geçer. Eski worker'ın elindeki kopya sonradan "failed" veya "sent" yazarsa yeni denemenin sonucunu ezer, durum yanlış olur. Token ile yalnızca satırı en son claim eden sonuç yazabilir. (Uygulama sırasında review'da yakalandı.)

## 17. Idempotency desteklenmiyorsa HTTP katmanı da isteği tekrar etmez

**Karar:** `PROVIDER_IDEMPOTENCY=false` iken provider isteğinin `GetBody`'si kaldırılır.

**Neden:** Go'nun `net/http` transport'u `Idempotency-Key` başlığı taşıyan ve gövdesi yeniden okunabilen bir isteği, yazıldıktan sonra kopan keep-alive bağlantıda sessizce yeniden gönderebilir. Provider dedup yapmıyorsa bu görünmez bir çift gönderim olur. Gövde geri sarılamaz olunca hata bize döner, "belirsiz" sınıflanır ve mesaj `unknown` olur (#3). Idempotent modda bu tekrar zararsızdır, açık bırakılır.

## 18. `Retry-After` 10 sn ile sınırlanır

**Karar:** 429'daki `Retry-After` (yoksa varsayılan bekleme) en fazla 10 sn kabul edilir; kanal bu süre durdurulur ve mesaj o kadar ertelenir.

**Neden:** Çok büyük bir `Retry-After` kanalı, worker'lara verilmiş işlerin kira süresinden (30 sn) uzun durdurursa bu işlerin kirası dolar ve çökme kurtarma yoluna (belirsiz) düşerler. Sınır, tek bir hatalı başlığın kanalı kilitlemesini de önler. Gerçek bir provider'da bu değer provider sözleşmesine göre ayarlanmalı.

## 19. Onay kararı tek transaction ve tek bağlantıda verilir

**Karar:** Onayda kampanya satırı `FOR UPDATE` ile kilitlenir; kural/içerik yeniden doğrulama, kitle sayımı, geniş kitle kontrolü, mesajların render edilip yazılması ve `sending`'e geçiş aynı transaction'da, aynı bağlantı üzerinde yapılır.

**Neden:** Kontrol ayrı bağlantıda yapılırsa ("kitle %50'den küçük mü?") karar ile kuyruğa alma arasında veri değişebilir. Ayrıca ilk uygulamada kilit tutulurken havuzdan ikinci bir bağlantı isteniyordu; eşzamanlı onaylarda küçük havuz tükenip kilitlenme (deadlock) oluşuyordu. Bunu `TestApproveSmallPoolNoDeadlock` yakalar.

## 20. Retry sonrası gönderilen mesajın hatası silinmez

**Karar:** `MarkSent` `last_error`'ı temizlemez. Dashboard'daki "Son hatalar" listesi her mesajın son hatasını güncel durumuyla (ör. `sent`, 2. denemede) gösterir.

**Neden:** Varsayılan hata oranlarıyla yüzlerce hata yaşanıp hepsi retry ile kurtarıldığında dashboard "Hata yok" diyordu; retry mekanizması görünmez oluyordu. Pazarlamacı ve operatör için "sorun yaşandı ama kurtarıldı" bilgisi değerli. Terminal hata sayıları zaten durum dağılımında ayrı görünür.

## 21. Paketleme: tek imaj, iki binary, distroless

**Karar:** Backend imajı `golang:1.24` ile derlenip `distroless/static` üzerine iki statik binary (`composer`, `mockprovider`) olarak konur. İmajda shell/curl olmadığından container healthcheck'leri binary'nin `healthcheck` alt komutuyla yapılır. `api` açılışta migration + boşsa seed çalıştırır; worker'lar `api` sağlıklı olunca başlar (migration sahibi tek servis). Compose'ta worker `deploy.replicas: 2`.

**Neden:** Küçük, saldırı yüzeyi az imaj; tek komutla sıralı ayağa kalkış; iki worker ile kanal liderliği ve rate limit davranışı gerçek ortamda gözlenebilir.
