# Deploy (um container)

Configuração Docker vive em [`../docker/`](../docker/).

```bash
cd docker
docker compose up --build -d
curl -sS http://localhost:8080/health
```

- Volume `mdcix-data` → `/data` (SQLite)
- Volume `mdcix-certs` → `/certs` (MITM opcional)
- Default: `mdcix serve --api --cache-dir /data`

Proxy mode:

```bash
docker compose run --service-ports mdcix serve --listen :8080 --cert-dir /certs --mode reader
```
