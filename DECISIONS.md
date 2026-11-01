# DECISIONS

Projedeki önemli kararlar ve gerekçeleri. Ayrıntılı tasarım: `docs/superpowers/specs/2026-10-29-smart-campaign-composer-design.md`.

## 1. Kuyruk olarak Postgres (`SKIP LOCKED`), Redis/Kafka yok

**Karar:** Mesaj kuyruğu Postgres'teki `messages` tablosu. Worker'lar `FOR UPDATE SKIP LOCKED` ile iş alır.

**Neden:**
- "Aynı kişiye aynı mesaj iki kez gitmez" kuralının ilk katmanı `UNIQUE (campaign_id, user_id, channel)` kısıtı. Onay + kuyruğa alma tek transaction'da. Ayrı bir kuyruk sistemi olsaydı "DB'ye yazdım ama kuyruğa push edemedim" (dual-write) sorunu çıkardı ve outbox gerekirdi.
- Ölçek (birkaç bin alıcı, kanal başı 100 msg/sn) Postgres için çok küçük.
- Daha az hareketli parça: tek bağımlılık.

**Bedeli / ne zaman değişir:** Gerçek Insider ölçeğinde (milyonlarca mesaj, çok sayıda instance) kuyruk Kafka/Redis Streams'e taşınmalı. Kuyruk erişimi şu an Postgres'e doğrudan bağlı: `queue.Store` somut bir tip, onay ve durum sorguları `messages` tablosunu doğrudan kullanıyor. Taşımadan önce bu erişim bir interface arkasına alınmalı. Provider tarafı zaten bir interface (`queue.Provider`).

## 2. Rate limit: kanal başına tek gönderici (advisory lock)

**Karar:** Her kanal için `pg_try_advisory_lock` ile bir lider worker seçilir. Lider bellekte eşit aralıklı bir hız sınırlayıcı tutar (100/sn; gönderimler arasında (1 sn + 50 ms)/hız beklenir, biriktirme ve patlama yoktur) ve gönderimleri goroutine'lerle paralel yapar.

**Neden:** Limit tek yerde sayılır, dağıtık sayaç (Redis vb.) gerekmez. Lider çökerse DB oturumu kapanır, kilit otomatik düşer, başka worker devralır.

**Bedeli:** Bir kanalın gönderim kapasitesi tek bir process ile sınırlı. 100/sn için fazlasıyla yeterli. Çok daha yüksek limitlerde dağıtık rate limiter gerekir.

- Limiter 50 ms güvenlik payı tutar (100 gönderim 1,05 sn'ye yayılır), bu yüzden efektif tavan ≈95/sn. Provider istekleri varış zamanına göre saydığı için, ağ gecikmesindeki ~50 ms'ye kadar oynamalarda provider tarafındaki her 1 sn penceresinde ≤100 korunur.
- Kilit bağlantısı koparsa liderlik devri sırasında eski liderin uçuştaki gönderimleri (en fazla `Concurrency` adet, her biri 5 sn timeout ile sınırlı) yeni liderin ilk gönderimleriyle kısa süre çakışabilir. Bu çakışma sınırlıdır; provider'ın 429'u yedek koruma sağlar. Lider her claim öncesi kilit bağlantısını kontrol ederek bu pencereyi daraltır.

## 3. Çift gönderim > hiç gitmeme: idempotency key

**Karar:** Çift SMS, hiç gitmemesinden daha kötü kabul edildi.
- Her provider isteği `Idempotency-Key: <message_id>` taşır. Mock provider aynı key'i ikinci kez görürse göndermez, ilk sonucu döner.
- Hatalar "kesin gitmedi" (bağlantı kurulamadı, 429, diğer 4xx) ve "belirsiz" (5xx, timeout, istek sırasında çökme) olarak ayrılır. Kesin gitmeyenlerden bağlantı hatası ve 429 güvenle tekrar denenir; diğer 4xx kalıcı hata sayılır ve mesaj tekrar denenmeden `failed` olur.
- Belirsiz durumda: provider idempotency destekliyorsa aynı key ile tekrar denenir. **Desteklemiyorsa mesaj `unknown` işaretlenir ve tekrar denenmez** (at-most-once).

- Deneme hakkı biten belirsiz mesajlar `failed` değil `unknown` olur, çünkü gitmiş olabilirler.

**Neden:** Exactly-once dış bir provider ile mümkün değil. Idempotency key pratikte "bir kez" sağlar. Olmadığı durumda pazarlama mesajı için kaçırılan bir mesaj, mükerrer mesajdan daha az zararlı.

## 4. 429 deneme hakkından düşmez, kanalı yavaşlatır

**Karar:** 429'da mesaj `Retry-After` kadar ertelenir ve attempt sayılmaz. Kanalın hız sınırlayıcısı aynı süre durdurulur; hız %70'e düşer ve her tam saniyede +10/sn artarak 100'e döner. Bir mesaj 20 kez 429 alırsa `failed` olur (#28).

**Neden:** 429 mesajla değil kanalın hızıyla ilgili ve mesajın gitmediği kesin. Kanalı yavaşlatmazsak sıradaki mesajlar da 429 alır.

## 5. İzin iki kez kontrol edilir

**Karar:** Onayda yalnızca izni olanlar için mesaj satırı açılır. Worker işi kuyruktan alırken, aynı SQL ifadesinde izni tekrar okur; geri çekilmişse `skipped_no_consent` yapar (çökmeden kurtarılan bir satırda ise `unknown`, çünkü önceki deneme gitmiş olabilir). Okuma ile gönderim arasında yalnızca hız sınırlayıcı beklemesi vardır (429 sonrası en fazla ~10 sn).

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
- Boş koşul listesi geçersiz; yalnızca açıkça `audience: "all"` ile ya da kitlenin tamamı eşlenemediğinde boş taslak olarak kabul edilir (#23).
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

**Karar:** `llm.Client` interface'inin `anthropic` ve `mock` implementasyonları var. Key yoksa sistem mock'a **düşmez**: LLM endpoint'leri `503 llm_not_configured` döner ve arayüzde kırmızı bant görünür. Mock testlerde ve yalnızca `LLM_PROVIDER=mock` açıkça verildiğinde çalışır. Bu durumda arayüzde kalıcı "MOCK MOD" bandı görünür ve kampanya `llm_provider='mock'` olarak işaretlenir. Mock her zaman geçerli, deterministik çıktı üretir; onarım döngüsü hazır (geçersiz dahil) yanıtlar döndüren ayrı bir test istemcisiyle (`llm.Scripted`) test edilir.

**Neden:** Key unutulursa sessiz fallback, sahte kampanya üretip gerçekmiş gibi gönderilmesine yol açar ve kimse fark etmez. Testler ise gerçek API olmadan deterministik çalışmalı.

## 15. Model seçimi: amaca göre, ölçerek

**Karar:** Model amaca göre ayrı seçilir: `ANTHROPIC_MODEL_SEGMENT` (varsayılan `claude-sonnet-5-5`) ve `ANTHROPIC_MODEL_CONTENT` (varsayılan `claude-sonnet-5-5`). Yapılandırılmış çıktı için structured output (JSON schema) kullanılır.

**Neden:** İki iş de kısa ve iyi tanımlı. Segment çıktısı her durumda backend doğrulamasından geçiyor, içerik limitlerini onarım döngüsü yakalıyor. Opus'un ek gücü bu işlerde maliyet ve gecikmeye değmez. Haiku 5.5 içerik için çok daha ucuz ve hızlı, ama Türkçe "samimi ama kısa" metin kalitesi ölçülmeden varsayılan yapılmadı. Bu yüzden iki model ölçülerek karşılaştırıldı.

**Eval (2026-11-01, `make eval`, 15 brief):** Sonnet 5.5 15/15 doğru kural (hepsi ilk denemede), 14/14 geçerli içerik (onarımsız), toplam 77 sn. Haiku 5.5 12/15 doğru kural, 14/14 geçerli içerik, toplam 95 sn. Haiku'nun üç hatası da koşulda değil `unmapped`'de: mesaj içeriğine ait ifadeleri ("memnuniyet anketi") kitle diye işaretleyip yanlış uyarı üretiyor. Sonuç: varsayılan Sonnet 5.5 kalır; Haiku burada ne daha doğru ne daha hızlı.

**Ölçüm (2026-10-30, örnek brief, canlı API, küçük örneklem):** İki model de kuralı ilk denemede doğru çıkardı (`abandoned_cart within_days 30` + `city in [İstanbul]`), onarım gerekmedi. Segment + içerik toplam süresi Sonnet 5.5'te 5.4–9.6 sn, Haiku 5.5'te 8.3–8.5 sn; yani Haiku burada belirgin hızlı değil. Haiku'nun metinleri doğru ama daha jenerik ("Sepetine dön ve alışverişini tamamla"); Sonnet'inkiler daha samimi ve brief'in tonuna daha yakın. Varsayılan Sonnet 5.5 kaldı. Maliyet öncelikliyse segment için Haiku env ile seçilebilir: kural backend doğrulamasından geçtiği için risk düşük. Sonnet ilk çalıştırmada `{{city}}'daki` yazdı; bu #22'yi doğurdu.

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

**Karar:** Onayda kampanya satırı `FOR UPDATE` ile kilitlenir; kural/içerik yeniden doğrulama, kitle sayımı, geniş kitle kontrolü, tekrar gönderim kontrolü (#27; onaylar ayrıca bir advisory lock ile sıraya girer), mesajların render edilip yazılması ve `sending`'e geçiş aynı transaction'da, aynı bağlantı üzerinde yapılır.

**Neden:** Kontrol ayrı bağlantıda yapılırsa ("kitle %50'den küçük mü?") karar ile kuyruğa alma arasında veri değişebilir. Ayrıca ilk uygulamada kilit tutulurken havuzdan ikinci bir bağlantı isteniyordu; eşzamanlı onaylarda küçük havuz tükenip kilitlenme (deadlock) oluşuyordu. Bunu `TestApproveSmallPoolNoDeadlock` yakalar.

## 20. Retry sonrası gönderilen mesajın hatası silinmez

**Karar:** `MarkSent` `last_error`'ı temizlemez. Dashboard'daki "Gönderim günlüğü" kartı her mesajın son hatasını güncel durumuyla (ör. `sent`, 2. denemede) gösterir.

**Neden:** Varsayılan hata oranlarıyla yüzlerce hata yaşanıp hepsi retry ile kurtarıldığında dashboard "Hata yok" diyordu; retry mekanizması görünmez oluyordu. Pazarlamacı ve operatör için "sorun yaşandı ama kurtarıldı" bilgisi değerli. Terminal hata sayıları zaten durum dağılımında ayrı görünür.

## 21. Paketleme: tek imaj, iki binary, distroless

**Karar:** Backend imajı `golang:1.24` ile derlenip `distroless/static` üzerine iki statik binary (`composer`, `mockprovider`) olarak konur. İmajda shell/curl olmadığından container healthcheck'leri binary'nin `healthcheck` alt komutuyla yapılır. `api` açılışta migration + boşsa seed çalıştırır; worker'lar `api` sağlıklı olunca başlar (migration sahibi tek servis). Compose'ta worker `deploy.replicas: 2`.

**Neden:** Küçük, saldırı yüzeyi az imaj; tek komutla sıralı ayağa kalkış; iki worker ile kanal liderliği ve rate limit davranışı gerçek ortamda gözlenebilir.

## 22. Değişkene Türkçe ek yapıştırılamaz

**Karar:** `{{city}}'daki`, `{{city}}'de`, `{{first_name}}'e` gibi değişkenin hemen ardından kesme işareti ya da harf gelen metinler `placeholder_suffix` doğrulama hatası verir. İçerik prompt'u da ek gerektirmeyen kalıpları (`{{city}} için`, `Merhaba {{first_name}},`) ister.

**Neden:** Türkçe ek ünlü uyumuna göre değişir. Değer bilinmediği için ek bazı alıcılarda yanlış olur ("İzmir'daki"), isim boşsa da ortada ek kalır ("Merhaba 'e"). Gerçek API ile ilk denemede Sonnet tam bunu yazdı. Kural hata olduğu için LLM'in onarım döngüsü düzeltir; pazarlamacının elle yazdığı metin de aynı kontrolden geçer. Ek render sırasında otomatik çekilebilirdi (İstanbul'da/İzmir'de), ama ünsüz sertleşmesi ve istisnalar bu case'in kapsamına göre fazla.

## 23. Karşılığı olmayan kitle: kural boş kalır, uydurulmaz

**Karar:** Brief'teki kitle ifadesinin hiçbir karşılığı yoksa ("VIP müşteriler") kural `filtered`, koşulsuz ve ifade `unmapped`'de olarak kabul edilir. Bu bir taslaktır: kimseyle eşleşmez (`FALSE`), `empty_audience` ve `unmapped` uyarıları görünür, pazarlamacı koşulu elle ekleyene kadar onay kapalıdır. Model çıktısı ayrıca `ValidateGenerated` ile kontrol edilir: `unmapped` doluyken `audience:"all"` reddedilir; her zaman da alan aralığının sınırındaki koşullar (`gte` minimum, `lte` maksimum; ör. `order_count gte 0`) reddedilir. Veride herkesi seçen koşullar için bkz. #26.

**Neden:** Canlı denemede model doğru davranıp kuralı boş bırakmıştı, ama doğrulama "en az bir koşul" istediği için onarım turu modeli herkesi seçen bir koşul uydurmaya itti. Sessizce herkese göndermek, eksik kuralı sormaktan çok daha kötü. Bu ek kontroller yalnızca modele uygulanır; pazarlamacı elle "Tüm kullanıcılar" seçebilir.

## 24. Model içeriği brief'te olmayan vaat veremez

**Karar:** Üretilen içerikteki her sayı (indirim oranı, tutar, gün, tarih) brief'te aynen geçmeli: brief "%30" diyorken "%50" yazılırsa `number_not_in_brief` hatası olur ("5.000" ile "5000" eşit sayılır). Ayrıca içerik stok ("stoklar tükenmeden"), kargo/teslimat, garanti, iade, hediye ve süre sınırı ("son gün", "24 saat içinde") ifadeleri için taranır. İfade brief'te (ya da aynı gruptan bir kelime: "kargo bedava" → "ücretsiz kargo") geçmiyorsa `unrequested_claim` hatası olur ve onarım döngüsüne döner. Kontrol yalnızca model çıktısına uygulanır; pazarlamacı elle istediğini yazabilir. Prompt'a da aynı kural eklendi.

**Neden:** Pazarlamacı brief'te stok ya da hızlı teslimat demediği halde model metne bu vaatleri ekledi; eval'de eski prompt'la 14 içerikten 2'sinde aynısı oldu. Gerçekte olmayan bir vaat (stok, teslimat süresi) müşteriye yanlış bilgi ve hukuki risk demek. Yalnızca prompt kuralı bunu azalttı ama sıfırlamadı, bu yüzden kontrol doğrulamada. Kelime listesi kaba bir sezgisel: her uydurmayı yakalamaz (ör. "en çok satanlar") ve nadiren yanlış pozitif verebilir; yanlış pozitifte onarım 2 turda düzelmezse hata kullanıcıya gösterilir ve metin elle düzenlenir.

## 25. Çökme kurtarma gerçek bir kill ile test edilir

**Karar:** Birim testleri çökmeyi veritabanında simüle eder (süresi geçmiş kira ile `sending` satır). Buna ek olarak `make chaos`, çalışan stack'te SMS lideri worker'ı gönderim ortasında `SIGKILL` ile öldürür ve devralma, kurtarma ve tek teslimatı provider'ın kendi sayaçlarıyla doğrular.

**Neden:** Simülasyon kurtarma mantığını test eder ama gerçek bir süreç ölümünü (yarıda kesilen HTTP istekleri, düşen advisory lock, başka process'in devralması) test etmez. Test iki modda da geçti: idempotent provider'da yarım kalanlar aynı key ile tekrar gönderilip tek teslimatta kaldı; idempotency yokken `unknown` oldular ve tekrar gönderilmediler.

## 26. Herkesi seçen koşul veriye bakılarak yakalanır

**Karar:** Bir koşul tek başına tüm kullanıcıları seçiyorsa (ör. kimsenin 500 siparişi yokken `order_count lte 500`, yani "en fazla 500 sipariş") etkisiz sayılır. Kitle önizlemesi her koşul için ayrı bir sorguyla bunu kontrol eder ve `ineffective_condition` ("Etkisiz koşul") uyarısı gösterir; bu, pazarlamacının elle eklediği koşullar için de geçerlidir. Model çıktısında aynı kontrol (`segment.IneffectiveCheck`, `Composer.RuleCheck` üzerinden) `tautological_condition` hatası olarak onarım döngüsüne döner.

**Neden:** Statik kontrol (#23) yalnızca alan aralığının sınırını (`gte 0`, `lte 1000`) yakalar; "en fazla 500 sipariş" aralık içinde olduğu halde herkesi seçer ve eşlenemeyen bir kitleyi ("VIP") gizleyebilir. Etkisiz bir koşulu kaldırmak tanımı gereği kitleyi değiştirmez, bu yüzden modelden kaldırmasını istemek güvenli. Kontrol veriye bağlı: bugün etkisiz olan koşul veri değişince daraltabilir; bu yüzden yalnızca uyarı ve model onarımı, elle eklenen koşul engellenmez.

## 27. Aynı metin aynı kişilere tekrar gidecekse onayda uyarı ve teyit

**Karar:** Onayda, son 30 günde onaylanmış başka bir kampanyanın **aynı brief'le** (boşluk ve büyük/küçük harf farkı yok sayılır) ya da aynı kanalda **birebir aynı metinle** (SMS metni; e-posta konu + gövde) bu kampanyanın kitlesindeki kişilere mesaj gönderip göndermediği kontrol edilir (`queued`, `sending`, `sent` ya da `unknown` mesajlar). Varsa onay ekranında hangi kampanyayla, kaç kişiye gittiği listelenir ve "Aynı kişilere tekrar göndermeyi onaylıyorum" kutusu işaretlenmeden onay verilmez. Backend aynı kontrolü onay transaction'ında tekrarlar (`confirm_duplicate`, aksi halde `422 duplicate_not_confirmed`).

**Neden:** Aynı brief'le ikinci kampanya açmak kolay ve sonucu (aynı kişiye aynı mesajın iki kez gitmesi) kullanıcı için rahatsız edici, marka için maliyetli. Engellemek yerine teyit istendi: bilinçli tekrar (hatırlatma) meşru olabilir. Yalnızca metin eşitliğine bakmak yetmez: Claude aynı brief'ten her seferinde biraz farklı metin üretir, bu yüzden aynı brief de eşleşme sayılır. Kesin kontrol, brief'i farklı yazılmış ama aynı amaçlı bir kampanyayı yakalamaz; bunun için isteğe bağlı LLM kontrolü var (#29). Daha geniş bir çözüm (kişi başına frekans sınırı, "son X günde kampanya almış olanları hariç tut" koşulu) kapsam dışı. Onaylar bir transaction advisory lock'u (`pg_advisory_xact_lock`) ile sıraya girer: aynı anda onaylanan iki aynı kampanyadan ikincisi, birincinin commit edilmiş mesajlarını görür ve teyit ister. Onaylar seyrek ve kısa olduğu için sıraya girmenin maliyeti yok.

## 28. 429 sınırsız beklemez: mesaj başına 20 429'dan sonra `failed`

**Karar:** Her mesajın kaç kez 429 aldığı `messages.rate_limited` sütununda sayılır. 429 deneme hakkından düşmez (#4), ama 20. 429'da mesaj `failed` olur (`last_error`: "... gave up after 20 rate-limited answers"). Sınır `Dispatcher.MaxRateLimited` ile değiştirilebilir.

**Neden:** 429 bir şey gönderilmediği anlamına gelir, bu yüzden deneme sayılmıyordu; ama sürekli 429 dönen bir provider'da mesaj sonsuza kadar kuyrukta kalıyor ve kampanya hiç bitmiyordu. 20, kısa bir hız sınırı fırtınasını atlatacak kadar büyük (her 429 kanalı `Retry-After` kadar durdurur; mesaj başına en az ~20 sn, pratikte çok daha uzun), kalıcı bir sorunu da birkaç dakikada görünür kılacak kadar küçük. `failed` doğru durum: 429'da mesajın gitmediği kesin.

## 29. Tekrar gönderim uyarısına isteğe bağlı LLM benzerlik kontrolü

**Karar:** `DUPLICATE_LLM_CHECK=true` (varsayılan `false`) iken onay ekranı açılırken (`GET /api/campaigns/{id}/duplicates`) Claude'a da sorulur:
- **Kimler karşılaştırılır:** yalnızca son 30 günde onaylanmış, bu kampanyanın kitlesindeki kişilere açık bir kanalda mesaj göndermiş ve brief'i ya da metni zaten birebir eşleşmemiş kampanyalar. En yeni 10 tanesi tek bir çağrıda gönderilir; kitlesi kesişmeyen kampanyalar hiç sorulmaz.
- **Ne sorulur:** her önceki brief için "aynı amaçlı mı" (aynı senaryo, aynı teklif, aynı duyuru) ve tek cümlelik Türkçe gerekçe. Çıktı yapılandırılmış şemayla istenir ve doğrulanır (her aday tam bir kez, bilinmeyen id yok, benzer olanın gerekçesi boş değil); hatada mevcut onarım döngüsü çalışır. Prompt'ta kampanya id'leri yerine `c1`, `c2` kullanılır, brief'ler güvenilmeyen veri olarak etiketlenir.
- **Uyarı:** benzer bulunanlar listede "benzer amaçlı bir SMS/e-posta" olarak, altında "Claude'a göre benzer: <gerekçe>" ile görünür. Arayüz bunlar için de tekrar gönderim kutusunu ister.
- **Hata:** Claude hata verir, zaman aşımına uğrar (20 sn) ya da key yoksa sonuç yalnızca kesin kontrolle döner (`similarity_check: "error"`), arayüz "benzer kampanya kontrolü yapılamadı" notu gösterir. Onay hiçbir durumda bu yüzden engellenmez.
- **Sunucu tarafı:** onay endpoint'i yalnızca kesin eşleşmeler (aynı brief, aynı metin) için `confirm_duplicate` ister. LLM sonucu sunucuda zorlanmaz.

**Neden:** Kesin kontrol (#27) farklı kelimelerle yazılmış aynı amaçlı brief'i yakalamaz; bu anlamsal bir karşılaştırma ve LLM'in iyi yaptığı bir iş. Her kampanyayla karşılaştırmak hem pahalı hem gereksiz: tekrar gönderim ancak aynı kişilere gidilmişse sorun, bu yüzden aday listesi önce veritabanında kitle kesişimiyle daraltılır. Kontrol isteğe bağlı ve varsayılan kapalı: her onay ekranında bir LLM çağrısı (gecikme ve maliyet) ekler ve yanlış pozitif verebilir. LLM'in kararı onay transaction'ında yeniden sorulmaz: transaction kampanya satırını kilitli tutarken dış bir ağ çağrısı yapılmamalı ve LLM'in kararı her çağrıda aynı olmayabilir. Bu yüzden LLM sonucu bir uyarıdır; zorunlu teyit yalnızca deterministik eşleşmelerde. Testler mock ile çalışır: mock iki brief'i, en az 4 harf/rakamlı kelimelerinde ortak kelime sayısı iki brief'in birleşim kelime sayısının en az %40'ıysa (Jaccard) benzer sayar ve ortak kelimeleri gerekçe olarak yazar.

## 30. Belirsiz bir denemeden sonra mesaj "kesin gitmedi" olarak bitmez

**Karar:** Bir denemenin sonucu belirsizse (5xx, timeout, gönderim ortasında worker çökmesi) mesaj satırında `maybe_delivered` işaretlenir. Sonraki bir denemede gelen "kesin gitmedi" türü sonuçlar artık `failed` ya da `skipped_no_consent` değil `unknown` olarak kaydedilir. Bu sonuçlar: bağlantı kurulamaması ve deneme hakkının bitmesi, kalıcı 4xx, 429 sınırına (#28) ulaşılması ve izin geri çekilmesi. `last_error`'a "an earlier attempt may have been delivered" notu eklenir; arayüz bunu "önceki deneme ulaşmış olabilir" olarak gösterir.

**Neden:** `failed` ve `skipped_no_consent` "bu kişiye gitmedi" demek: dashboard'da "Gitmeyenler" filtresi ve tekrar gönderim kontrolü (#27) buna güvenir. Oysa ilk deneme provider'a ulaşıp cevabı kaybolmuş olabilir. Bu durumda mesajı `failed` saymak hem raporu yanıltır hem de aynı metnin aynı kişiye tekrar gönderilmesine kapı açar. Çökmeden kurtarılan satırlar için bu kural zaten vardı; review'da, belirsiz cevaptan sonra tekrar denenen satırlarda eksik olduğu bulundu.

