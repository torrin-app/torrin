#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export KO_DOCKER_REPO=ko.local

services=(api ingest stream stremio webdav byos telegram catalog scheduler)
[ $# -gt 0 ] && services=("$@")

for svc in "${services[@]}"; do
	if [ "$svc" = "ingest" ]; then
		echo ">> building ingest (Dockerfile — bundles par2 for usenet repair)"
		docker build -f deploy/Dockerfile.ingest -t torrin/ingest:latest .
		echo "   -> torrin/ingest:latest"
		continue
	fi
	if [ "$svc" = "hdencode-solver" ]; then
		echo ">> building hdencode-solver (Dockerfile — Camoufox browser)"
		docker build -f deploy/Dockerfile.hdencode-solver -t torrin/hdencode-solver:latest .
		echo "   -> torrin/hdencode-solver:latest"
		continue
	fi
	echo ">> building $svc"
	img=$(go run github.com/google/ko@latest build --local --base-import-paths "./$svc/cmd" | tail -n1)
	docker tag "$img" "torrin/$svc:latest"
	echo "   -> torrin/$svc:latest"
done
echo "done. now: docker compose -f deploy/docker-compose.yml up -d"
