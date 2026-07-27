# Torrin

Open-source, self-hostable debrid + streaming service. Add a magnet, NZB, or link — get an instant HTTPS stream. No local torrent client, no seeding, no IP exposure.

## What it does

Torrin downloads torrents, Usenet, and file-host links on remote servers (behind a VPN), caches the result in S3-compatible object storage, and serves it back as signed HTTPS stream URLs. Cached content is shared across users, so repeat plays are instant.

- **Instant streaming** for cached content; downloads behind a VPN when it isn't.
- **Multiple sources** — torrents (qBittorrent), debrid providers, Usenet (NZB), file hosters, and scene/HDEncode releases.
- **Stremio addon**, **WebDAV** mount (VLC / Infuse / Kodi), and a web UI.
- **RSS auto-download** with criteria filters (resolution, source, codec, HDR, group, …).
- **BYOS** — Bring Your Own Storage: pipe downloads to your own cloud via rclone.
- **Plan-based limits** — concurrency, per-download size caps, priority, and a cache-eviction budget.

## How it works

1. A user submits a magnet / NZB / link, or searches for a title.
2. `api` checks the cache and content providers — if available, returns signed stream URLs instantly.
3. If not, a job is queued and `ingest` downloads it (behind a VPN).
4. On completion, the files are published to the shared object store.
5. The user streams via a signed HTTPS URL from `stream`, or mounts the library via `webdav`.

## Architecture

Torrin is a Go monorepo of small services communicating over [NATS](https://nats.io), backed by PostgreSQL (metadata) and a filesystem-backed object store (served over S3 via [rclone](https://rclone.org) for the remote cache tier).

| Service | Role |
|---|---|
| `api` | REST API — auth, jobs, billing, RSS, reseller, storage OAuth |
| `ingest` | Download pipeline — torrent / debrid / Usenet / hoster / release → publish to S3 |
| `stream` | Signed HTTPS streaming of cached files |
| `webdav` | WebDAV server for mounting the library |
| `catalog` | Content catalog / metadata |
| `scheduler` | Background scheduling + cache eviction |
| `stremio` | Stremio addon server |
| `byos` | Bring Your Own Storage (rclone) |
| `telegram` | Telegram bot |
| `shared/` | Shared core — auth, jobs, eviction, providers, qbit, rss, safety, storage, usenet, … |
| `deploy/` | Docker Compose stack + Caddy config |

The Stremio addon (`comet`) lives in a separate repository.

## Self-hosting

Requirements: Docker + Docker Compose, a domain, and a VPN provider for the download egress. The stack bundles the object store, PostgreSQL, NATS, qBittorrent, and gluetun (VPN).

```bash
git clone https://github.com/torrin-app/torrin
cd torrin/deploy
# create deploy/.env with the required secrets (see the compose files for the full list)
docker compose up -d
```

See [`deploy/`](deploy/) for the full stack and overlay files (`docker-compose.{vpn,public,comet}.yml`).

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). Please report security issues via [SECURITY.md](SECURITY.md), not a public issue.

## License

[AGPL-3.0](LICENSE). If you run a modified version of Torrin as a network service, you must make your source changes available to its users.
