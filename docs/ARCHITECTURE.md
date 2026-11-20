# Arquitetura mdcix

```text
CLI / REST / MCP / proxy
          │
          ▼
     gateway.Engine
          │
   ┌──────┴──────┐
   │ policy      │  robots aviso (local); sem bypass de paywall/CAPTCHA
   │ security    │  SSRF + classificação de falha
   │ fetch/HTTP  │  Accept markdown → HTML fallback
   │ convert     │  extração reader|agent
   │ document    │  seções, versão, tokens
   │ cache       │  SQLite + ETag/TTL/evict
   │ context     │  BM25 + budget packer
   │ telemetry   │  off por padrão
   └─────────────┘
```

Um único motor (`internal/gateway`) alimenta:

| Interface | Entrada |
|-----------|---------|
| CLI | `outline`, `read`, `fetch`, `mcp`, `serve` |
| REST | `POST /v1/outline\|read\|sections\|read-budget` |
| MCP stdio | tools homônimas |
| Proxy | conversão HTML elegível; JSON/SSE pass-through |

## Pacotes

Alinhados à proposta, adaptados ao repo existente (proxy/mitm/certs/convert preservados):

- `internal/document` — modelo versionado + segmentação
- `internal/context/{ranking,budget,tokenizer}` — seleção lexical
- `internal/cache` — SQLite local
- `internal/policy` — preferências de recuperação local
- `internal/security` — SSRF + falhas
- `internal/telemetry` — opt-in local
- `internal/transport/{http,mcp}` — REST + MCP
- `internal/proxy` — forward proxy (permanece fora de `transport/` nesta fase)

## Métricas (distinção obrigatória)

1. **Extração:** `original_tokens` → `markdown_tokens`
2. **Seleção:** `markdown_tokens` → `returned_tokens` (budget/sections)
3. **Tarefa de agente:** só no harness `benchmarks/` (não inferir de tamanho de página)

Estimativas usam `cl100k_base` — **não** são billing do provedor.
