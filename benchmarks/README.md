# mdcix benchmarks

Primary validation for budget-aware retrieval.

## Goals

Compare task-level token use and answer accuracy across:

1. Raw HTML  
2. Full Markdown (mdcix)  
3. Agent-selected progressive retrieval (outline → sections)  
4. Engine-selected `read_budget`  
5. Optional external baselines (Jina/Firecrawl) via env  

## Status

Harness skeleton — fixtures and runners land next. Do **not** count extraction failures as savings.

```bash
# placeholder
go test ./internal/...
```
