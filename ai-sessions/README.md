# ai-sessions

Bu projenin geliştirildiği Claude Code oturumunun kayıtları.

- `00-build-full.md`: oturumun baştan sona tam hali. Oturum kaydından (jsonl) üretildi, çünkü `/export` yalnızca son context sıkıştırmasından sonrasını veriyor. Kullanıcı mesajları ve Claude'un yanıtları birebir; araç çağrıları tek satır özet olarak var, araç çıktıları çıkarıldı.
- `01-build.md`: aynı oturumun `/export` çıktısı (son bölüm, araç çıktılarıyla birlikte).
- Akış: brainstorm → spec (`docs/superpowers/specs/`) → plan (`docs/superpowers/plans/`) → görev başına alt ajan ile uygulama + bağımsız review.
- Export'lardan API key'ler, token'lar ve kişisel bilgiler (e-posta, yerel kullanıcı yolları) temizlenmiştir.
