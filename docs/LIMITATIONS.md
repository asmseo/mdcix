# Limitações conhecidas

- **BM25 é lexical**, não semântico multilíngue. Cross-language → preferir outline + `read_sections`.
- **Sem embeddings / runtime de modelo** no motor (de propósito).
- **robots.txt** é aviso best-effort; **não** autoriza redistribuição pública.
- **Content Signals / licenças de republicação** reservados; não há diretório público.
- **Cache** é local e por processo/volume; sem cache compartilhado autenticado.
- **MITM HTTPS** é opt-in explícito (flag); TLS upstream não é enfraquecido por padrão além de `--insecure`.
- **Proxy** não intercepta JSON/SSE/binário; só HTML/Markdown elegíveis.
- **Falhas** (CAPTCHA, vazio, 4xx) **não** contam como economia de tokens.
- **`--allow-private`** só para teste local; desliga a guarda SSRF de loopback/privado.
- **Telemetria** off por padrão; sem export remoto neste release.
- **Benchmark** local mede tokens/latência de retrieval; acurácia de resposta LLM exige modelo externo (opcional, env-gated).
- **Version pin** avisa mismatch; não há store multi-versão histórico além do snapshot em cache.
