# mdcix benchmarks

Primary validation for budget-aware retrieval.

## Goals

Compare task-level token use (and later answer accuracy) across:

1. Raw HTML  
2. Full Markdown (mdcix)  
3. Agent-selected progressive retrieval (outline → sections)  
4. Engine-selected `read_budget`  
5. Optional external baselines (Jina/Firecrawl) via env  

## Local harness (no external network)

```bash
go test ./benchmarks/ -v -count=1
```

- Fixtures: `fixtures/` (EN/PT/ES, unstructured, bot-challenge)
- Tasks: `tasks.json` with `expect_facts` / `expect_failure`
- Reports: html/md/returned tokens, latency, reduction vs Markdown, fact presence

**Rules**

- Extraction failures / CAPTCHA / bot walls are **not** counted as token savings.
- Token counts are `cl100k_base` estimates — not provider billing.
- Do not optimize fixtures to artificially favor mdcix.

## Optional external baselines

```bash
export MDCIX_BENCH_JINA_URL=...      # optional
export MDCIX_BENCH_FIRECRAWL_KEY=... # optional
# runners for these envs are reserved; local harness does not require them
```

## LLM answer scoring

Out of band: same model/prompt across strategies. Not required for the local token harness.
