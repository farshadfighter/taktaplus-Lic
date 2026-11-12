#!/bin/sh
# Builds the signed offline bundle dist/taktaplus-lic-VERSION.tar.gz.
#   scripts/release/build-bundle.sh VERSION RELEASE_KEY [MIN_FROM]
# RELEASE_KEY is an encrypted key from `lictool keygen` whose public key is
# in internal/releasekeys/keys/ (and so built into this version); the key
# passphrase is read from $TPLIC_KEY_PASSPHRASE. LICTOOL names a lictool
# binary (default: built from this checkout).
set -eu
VERSION=$1 KEY=$2 MIN_FROM=${3:-}
cd "$(dirname "$0")/../.."
if [ -z "${LICTOOL:-}" ]; then
	LICTOOL=$(mktemp -d)/lictool
	go build -o "$LICTOOL" ./cmd/lictool
fi
ls internal/releasekeys/keys/*.pub >/dev/null 2>&1 || { echo "no release public key in internal/releasekeys/keys/" >&2; exit 1; }

IMAGES="taktaplus-lic:$VERSION postgres:16-alpine docker:27-cli"
docker build --build-arg VERSION="$VERSION" --build-arg COMMIT="$(git rev-parse --short HEAD)" \
	--build-arg DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" -t "taktaplus-lic:$VERSION" .
for i in $IMAGES; do
	case "$i" in taktaplus-lic:*) ;; *) docker image inspect "$i" >/dev/null 2>&1 || docker pull "$i" ;; esac
done

OUT=$(mktemp -d)
trap 'rm -rf "$OUT"' EXIT
mkdir -p "$OUT/images" "$OUT/scripts" dist
# shellcheck disable=SC2086
docker save -o "$OUT/images/images.tar" $IMAGES
chmod 644 "$OUT/images/images.tar"
cp deploy/bundle/docker-compose.yml deploy/bundle/install.sh "$OUT/"
cp deploy/bundle/scripts/*.sh "$OUT/scripts/"
chmod 755 "$OUT/install.sh" "$OUT"/scripts/*.sh
"$LICTOOL" build-release -key "$KEY" -dir "$OUT" -product taktaplus-lic -version "$VERSION" -min-from "$MIN_FROM" \
	-images "$(echo $IMAGES | tr ' ' ,)" -notes "${NOTES:-}" -out "dist/taktaplus-lic-$VERSION.tar.gz"
