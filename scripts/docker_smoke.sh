#!/usr/bin/env bash
# Builds the image for this machine, runs it the way deploy/compose.yaml does and waits for Docker to call it healthy.
# Local and CI use; nothing is pushed.
set -euo pipefail
cd "$(dirname "$0")/.."
image=oriel:smoke
name=oriel-smoke-$$
trap 'docker rm -f "$name" >/dev/null 2>&1 || true; docker volume rm "$name-data" >/dev/null 2>&1 || true' EXIT
docker build -t "$image" .
# A throwaway password, only for this container.
docker run -d --name "$name" -e ORIEL_PASSWORD="smoke-$$-$RANDOM" -p 127.0.0.1::7470 \
  --read-only --cap-drop ALL --security-opt no-new-privileges:true -v "$name-data:/data" "$image" >/dev/null
for _ in $(seq 1 30); do
  state="$(docker inspect -f '{{.State.Health.Status}}' "$name")"
  [[ "$state" == healthy ]] && break
  sleep 2
done
if [[ "$state" != healthy ]]; then
  echo "The container is $state, not healthy." >&2
  docker logs "$name" >&2
  exit 1
fi
port="$(docker port "$name" 7470/tcp | head -1 | sed 's/.*://')"
curl -fsS "http://127.0.0.1:$port/health"
echo
