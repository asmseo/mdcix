# Testar o mdcix ponto a ponto

Branch recomendada: `cursor/context-aware-engine-836d`

Você pode **parar a qualquer momento** e validar só o bloco atual. Cada passo tem: comando → o que olhar → critério de OK.

Atalho automatizado (fixtures locais, sem rede externa):

```bash
./scripts/smoke-local.sh
```

---

## 0. Build

```bash
go build -o mdcix ./cmd/mdcix
./mdcix version
go test ./... -count=1
```

OK: binário sobe, `go test` verde (inclui `benchmarks/`).

---

## 1. Fixture local (recomendado)

Em um terminal:

```bash
python3 -m http.server 18082 --bind 127.0.0.1 --directory benchmarks/fixtures
```

URL de exemplo:

```text
http://127.0.0.1:18082/en-docs-structured.html
```

> Loopback é bloqueado por SSRF por padrão. Nos testes locais use `--allow-private`.

---

## 2. CLI — fetch (compatibilidade)

```bash
./mdcix fetch --allow-private --mitm=false \
  http://127.0.0.1:18082/en-docs-structured.html
```

OK:

- stdout tem Markdown com Overview/Pricing
- stderr tem comparação de tokens (`original` / `markdown` / `saved`)

---

## 3. CLI — outline

```bash
./mdcix outline --allow-private --mitm=false --cache-dir ./data \
  http://127.0.0.1:18082/en-docs-structured.html
```

OK: JSON com `document_version`, `total_tokens`, `sections[]` com `id`/`title`/`tokens`.

Anote um `id` (ex.: seção de pricing) e o `document_version`.

---

## 4. CLI — read (3 modos)

```bash
# página completa
./mdcix read --allow-private --mitm=false --cache-dir ./data \
  http://127.0.0.1:18082/en-docs-structured.html

# seções escolhidas pelo agente
./mdcix read --allow-private --mitm=false --cache-dir ./data \
  http://127.0.0.1:18082/en-docs-structured.html \
  --sections pricing --budget 800 --json

# budget lexical (BM25)
./mdcix read --allow-private --mitm=false --cache-dir ./data \
  http://127.0.0.1:18082/en-docs-structured.html \
  --query "pricing integrations" --budget 400 --json
```

OK:

- full: Markdown + stderr com `document_version` / tokens
- sections: `selected_sections` contém o id pedido; ordem do documento
- budget: `returned_tokens` ≤ budget (+ margem mínima); `selected_sections` / `omitted_sections`

---

## 5. Cache SQLite

```bash
./mdcix cache stats --cache-dir ./data
./mdcix outline --allow-private --mitm=false --cache-dir ./data \
  http://127.0.0.1:18082/en-docs-structured.html >/dev/null
./mdcix cache stats --cache-dir ./data
./mdcix cache clear --cache-dir ./data
./mdcix cache stats --cache-dir ./data
```

OK: `entries` sobe após outline/read; volta a `0` após clear.

---

## 6. REST API

```bash
./mdcix serve --api --listen 127.0.0.1:18080 \
  --allow-private --mitm=false --cache-dir ./data
```

Em outro terminal:

```bash
curl -sS http://127.0.0.1:18080/health

curl -sS -X POST http://127.0.0.1:18080/v1/outline \
  -H 'content-type: application/json' \
  -d '{"url":"http://127.0.0.1:18082/en-docs-structured.html"}'

curl -sS -X POST http://127.0.0.1:18080/v1/read \
  -H 'content-type: application/json' \
  -d '{"url":"http://127.0.0.1:18082/en-docs-structured.html","format":"markdown"}'

curl -sS -X POST http://127.0.0.1:18080/v1/sections \
  -H 'content-type: application/json' \
  -d '{"url":"http://127.0.0.1:18082/en-docs-structured.html","section_ids":["pricing"],"budget":800}'

curl -sS -X POST http://127.0.0.1:18080/v1/read-budget \
  -H 'content-type: application/json' \
  -d '{"url":"http://127.0.0.1:18082/en-docs-structured.html","query":"pricing plans","budget":400}'
```

OK: `/health` → `{"status":"ok"}`; demais endpoints devolvem JSON/Markdown coerente com o CLI.

---

## 7. Segurança SSRF

```bash
# SEM --allow-private → deve falhar
./mdcix outline --mitm=false \
  http://127.0.0.1:18082/en-docs-structured.html
```

OK: erro bloqueando loopback/privado.

---

## 8. Bot challenge ≠ savings

```bash
./mdcix outline --allow-private --mitm=false \
  http://127.0.0.1:18082/bot-challenge.html
```

OK: erro `bot_challenge` (não inventar economia de tokens).

---

## 9. MCP stdio

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"manual","version":"1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
| ./mdcix mcp --allow-private --mitm=false
```

OK: `initialize` com `serverInfo.name=mdcix`; `tools/list` traz `page_outline`, `read_page`, `read_sections`, `read_budget`.

Cursor / Claude Desktop:

```json
{
  "mcpServers": {
    "mdcix": {
      "command": "/caminho/absoluto/mdcix",
      "args": ["mcp", "--allow-private", "--cache-dir", "/caminho/data"]
    }
  }
}
```

(`--allow-private` só se for testar URLs locais.)

---

## 10. Proxy HTTP

```bash
./mdcix serve --listen 127.0.0.1:18081 --mitm=false --mode reader
```

```bash
# HTML → Markdown + headers
curl -sS -D - -x http://127.0.0.1:18081 \
  http://127.0.0.1:18082/en-docs-structured.html | head

# JSON deve passar intacto (subir um origin JSON separado se quiser)
```

OK:

- HTML vira `text/markdown` com `X-Mdcix-*`
- JSON / SSE **não** são convertidos (pass-through)

---

## 11. Benchmarks locais

```bash
go test ./benchmarks/ -v -count=1
```

OK: tabela html/md/budget/reduction; `bot-challenge` rejeitado.

---

## 12. Docker (opcional)

```bash
cd docker && docker compose up --build -d
curl -sS http://localhost:8080/health
```

OK: um container; volume `/data` para SQLite; API no 8080.

---

## Ordem sugerida se for testar “na mão”

1. Build + `go test`
2. Fixture server
3. `fetch` → `outline` → `read` (3 modos)
4. `cache stats/clear`
5. REST (`serve --api`)
6. SSRF + bot-challenge
7. MCP handshake
8. Proxy convert + pass-through
9. Benchmarks
10. Docker (se tiver)

Qualquer passo vermelho: pare ali — o smoke (`./scripts/smoke-local.sh`) imprime `PASS/FAIL` por item no mesmo espírito.
