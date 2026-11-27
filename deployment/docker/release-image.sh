#!/usr/bin/env bash
set -euo pipefail
ref="${IMAGE:?}:${VERSION:?}"
errors=$(mktemp)
trap 'rm -f "$errors"' EXIT

if ! digest=$(docker buildx imagetools inspect "$ref" --format '{{.Manifest.Digest}}' 2>"$errors"); then
  if ! grep -Eiq 'manifest unknown|name unknown|: not found|404 Not Found' "$errors"; then
    cat "$errors" >&2
    exit 1
  fi
  sources=()
  for arch in amd64 arm64; do
    digest=$(cat "digests/$arch.digest")
    [[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]]
    docker buildx imagetools inspect "$IMAGE@$digest" --format '{{json .Image}}' |
      jq -e --arg arch "$arch" '.os == "linux" and .architecture == $arch' >/dev/null
    sources+=("$IMAGE@$digest")
  done
  docker buildx imagetools create --tag "$ref" \
    --annotation "index:org.opencontainers.image.revision=${REVISION:?}" \
    --annotation "index:org.opencontainers.image.version=$VERSION" \
    "${sources[@]}"
  digest=$(docker buildx imagetools inspect "$ref" --format '{{.Manifest.Digest}}')
fi

[[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]]
docker buildx imagetools inspect --raw "$IMAGE@$digest" | jq -e --arg revision "$REVISION" --arg version "$VERSION" '
  .annotations["org.opencontainers.image.revision"] == $revision and
  .annotations["org.opencontainers.image.version"] == $version and
  ([.manifests[].platform | .os + "/" + .architecture] | sort == ["linux/amd64", "linux/arm64"])
' >/dev/null || { echo "Release image $ref does not match the tagged revision, version or platforms." >&2; exit 1; }
sed "s|ghcr.io/plumbral/yaare:latest|$ref@$digest|" deployment/docker/compose.yaml > installers/compose.yaml
[[ "$(env -u YAARE_IMAGE docker compose -f installers/compose.yaml config --images)" == "$ref@$digest" ]]
