# mdcix benchmarks

Primary validation for budget-aware retrieval.

## Goals

Compare task-level token use and answer accuracy across:

1. Raw HTML  
2. Full Markdown (mdcix)  
3. Agent-selected progressive retrieval (outline → sections)  
4. Engine-selected `read_budget`  
5. Optional external baselines (Jina/Firecrawl) via env  

## Local harness (no external network)

Fixtures under `fixtures/` cover EN/PT/ES, structured docs, unstructured small-business pages, and a bot-challenge negative case.

```bash
go test ./benchmarks/ -v -count=1
```

The harness reports `html_tokens`, `md_tokens`, `budget_tokens`, approximate outline size, and reduction vs full Markdown.

**Rules**

- Extraction failures / CAPTCHA / bot walls are **not** counted as token savings.
- Token counts are `cl100k_base` estimates — not provider billing.
- External model answer-accuracy runs remain optional and env-gated.

## Status

Local token-reduction harness is runnable. Live-web and LLM answer scoring land later; do not optimize fixtures to artificially favor mdcix.
