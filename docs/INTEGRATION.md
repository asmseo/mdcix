# Integração mdcix com agentes de IA

O caminho **universal** é o proxy HTTP local. Qualquer runtime que respeite `HTTP_PROXY` / `HTTPS_PROXY` passa a receber Markdown limpo.

```bash
mdcix serve --listen :8080
export HTTP_PROXY=http://127.0.0.1:8080
export HTTPS_PROXY=http://127.0.0.1:8080
# Para HTTPS com MITM, confie em ./certs/ca-cert.pem
```

Alternativa one-shot (tool/CLI):

```bash
mdcix fetch https://example.com
```

Headers de comparação (sempre presentes nas respostas convertidas):

| Header | Significado |
|--------|-------------|
| `X-Mdcix-Tokens-Original` | tokens do HTML/corpo original |
| `X-Mdcix-Tokens-Markdown` | tokens do Markdown |
| `X-Mdcix-Tokens-Saved` | economia absoluta |
| `X-Mdcix-Saved-Percent` | economia % |
| `X-Mdcix-Source` | `native` ou `converted` |

Ver README principal para Grok, Claude, ChatGPT, Gemini, Grok Bot e Cursor.
