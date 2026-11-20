# mdcix — Context-Aware Web Infrastructure

Infraestrutura **local-first**, independente de modelo, para consumo **budget-aware** da web por agentes de IA.

Markdown é o formato inicial de representação — **não** o produto. O produto é recuperação seletiva e mensurável: página completa, outline, seções ou packing por orçamento de tokens.

Licença do núcleo: **MIT** (open-core). Sem billing, cloud obrigatória ou telemetria ligada por padrão.

## Hipótese

Recuperação seletiva e budget-aware pode reduzir tokens totais da tarefa **sem** degradar acurácia da resposta. O harness em `benchmarks/` existe para medir isso — não assumir vitória.

## Por que existe

Agentes gastam tokens em HTML ruidoso. O mdcix:

1. Negocia `Accept: text/markdown` e extrai conteúdo principal.
2. Segmenta em seções endereçáveis (versão de documento estável).
3. Expõe outline / multi-seção / `read --query --budget` (BM25 lexical).
4. Reporta tokens (original vs markdown vs retornado) — falhas/bot walls **não** contam como economia.

## CLI

```bash
mdcix fetch https://example.com
mdcix outline https://example.com
mdcix read https://example.com
mdcix read https://example.com --sections pricing,integrations --budget 2000
mdcix read https://example.com --query "pricing plans" --budget 1500
mdcix serve                 # proxy HTTP_PROXY
mdcix serve --api           # REST /v1/outline|/v1/read|/v1/sections|/v1/read-budget
mdcix mcp                   # MCP stdio (page_outline|read_page|read_sections|read_budget)
mdcix cache stats --cache-dir ./data
mdcix cache clear --cache-dir ./data
```

Cache SQLite (opcional): `--cache-dir ./data` no CLI/REST/MCP.

## Integração universal = proxy

```bash
mdcix serve --listen :8080
export HTTP_PROXY=http://127.0.0.1:8080 HTTPS_PROXY=http://127.0.0.1:8080
curl -x http://127.0.0.1:8080 -D - https://example.com -o /tmp/out.md
```

## Contagem de tokens (obrigatório no MVP)

Toda resposta processada inclui:

| Header | Significado |
|--------|-------------|
| `X-Mdcix-Tokens-Original` | tokens do corpo original (HTML etc.) |
| `X-Mdcix-Tokens-Markdown` | tokens do Markdown entregue |
| `X-Mdcix-Tokens-Saved` | economia absoluta (original − markdown) |
| `X-Mdcix-Saved-Percent` | economia em % |
| `X-Mdcix-Source` | `native` (já era MD) ou `converted` |

- **`mdcix fetch`**: imprime o mesmo resumo em stderr (`--- mdcix token comparison ---`).
- **`mdcix serve`**: loga por request `original=… markdown=… saved=… (…%)` e um total na saída.

Encoding padrão: `cl100k_base` (tiktoken-compat).

## Como usar com Grok / Claude / ChatGPT / Gemini

1. Rode `mdcix serve` na máquina (ou container) onde o agente faz HTTP.
2. Configure o ambiente (ou o runtime do agente) com:

```bash
export HTTP_PROXY=http://127.0.0.1:8080
export HTTPS_PROXY=http://127.0.0.1:8080
```

3. Para HTTPS interceptado (MITM), confie na CA local `./certs/ca-cert.pem`.
4. Se o produto **não** respeitar proxy env, use a tool/CLI:

```bash
mdcix fetch "https://docs.example.com/page"
```

Exemplo de verificação manual (útil para colar num system prompt / tool):

```bash
curl -sS -x http://127.0.0.1:8080 -D - https://example.com -o /tmp/page.md \
  | grep -i X-Mdcix
```

## Grok Bot

- Preferência: proxy env no host/processo do bot (`HTTP_PROXY` / `HTTPS_PROXY` → `http://127.0.0.1:8080`).
- Se o bot só tiver tools de fetch/URL: registre um comando que rode `mdcix fetch <url>` e devolva o stdout (Markdown) + stats do stderr.
- Headers `X-Mdcix-*` permitem auditar economia por chamada.

## Cursor (agentes / cloud agents)

- No ambiente do agent: defina `HTTP_PROXY` e `HTTPS_PROXY` para o mdcix local (ou sidecar no mesmo pod/rede).
- Alternativa robusta: o agent chama `mdcix fetch <url>` via shell/tool e usa o Markdown no contexto.
- Cloud agents: rode o proxy no mesmo workspace (`mdcix serve`) antes dos fetches; ou invoque `fetch` one-shot em cada URL.

```bash
# no workspace Cursor
mdcix serve --listen :8080 &
export HTTP_PROXY=http://127.0.0.1:8080 HTTPS_PROXY=http://127.0.0.1:8080
# …agent tools que fazem HTTP passam a ver Markdown
```

## MCP (stdio)

O mesmo `gateway.Engine` do CLI/REST é exposto via Model Context Protocol:

```bash
mdcix mcp
```

Tools: `page_outline`, `read_page`, `read_sections`, `read_budget`. Conteúdo web volta delimitado como material não confiável.

Exemplo Cursor / Claude Desktop (`mcp.json`):

```json
{
  "mcpServers": {
    "mdcix": {
      "command": "mdcix",
      "args": ["mcp"]
    }
  }
}
```

Estratégia: outline quando seções são desconhecidas; multi-seção quando ids são conhecidos; budget para queries lexicais same-language; página completa só em docs curtos.

## Install

```bash
git clone https://github.com/asmseo/mdcix.git
cd mdcix
go build -o mdcix ./cmd/mdcix
./mdcix serve
```

Docker (um container, volume SQLite em `/data`):

```bash
cd docker && docker compose up --build -d
curl -sS -X POST http://localhost:8080/health
curl -sS -X POST http://localhost:8080/v1/outline -H 'content-type: application/json' \
  -d '{"url":"https://example.com"}'
# proxy mode: override command → serve --listen :8080 --cert-dir /certs
```

Benchmark local (sem rede externa):

```bash
go test ./benchmarks/ -v -count=1
```

Config de exemplo: [`examples/config.yml`](examples/config.yml).

## Modos de conversão

| Modo | Comportamento |
|------|----------------|
| `reader` (default) | extrai `main`/`article`, remove chrome — máxima economia |
| `agent` | artigo + seção `## Links` (URLs absolutas) — melhor navegação |

```bash
mdcix serve --mode agent
mdcix fetch --mode agent https://example.com
```

## Arquitetura (resumo)

```
Agente / curl / tool
        │  HTTP_PROXY → :8080
        ▼
   mdcix proxy
     1) Accept: text/markdown …
     2) fetch upstream (HTTP ou MITM HTTPS)
     3) nativo MD → passa; HTML → extrai + converte
     4) conta tokens original vs markdown
     5) headers X-Mdcix-* + body text/markdown
```

Pacotes: `cmd/mdcix`, `internal/{gateway,document,context,cache,security,transport/{http,mcp},proxy,mitm,certs,fetch,convert,tokens,stats,config,pro}`.

## Open-core / Pro

Core MIT funciona sozinho (proxy, CLI, REST, MCP stdio). `internal/pro` só tem interfaces/stubs (`Features`, `License`, `MCPBridge` overlay). Placeholders futuros: cache avançado, regras por domínio, dashboard, auth de equipe, Chrome — **sem billing** nesta fase.

## Desenvolvimento

```bash
go test ./...
go run ./cmd/mdcix fetch https://example.com
```

## Licença

MIT — ver [LICENSE](LICENSE).
