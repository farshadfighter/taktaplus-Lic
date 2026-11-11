#!/bin/sh
# Installs the TaktaPlus license server from this extracted offline bundle.
#   sudo ./install.sh PUBLIC_URL
# PUBLIC_URL is the external address installations reach, for example
# https://license.example.com. TPLIC_HOME chooses the installation directory.
set -eu
SRC=$(cd "$(dirname "$0")" && pwd)
HOME_DIR=${TPLIC_HOME:-/opt/taktaplus-lic}
. "$SRC/scripts/product.sh"
. "$SRC/scripts/common.sh"

[ "$(id -u)" = 0 ] || die "run as root"
command -v docker >/dev/null || die "Docker Engine is not installed"
docker compose version >/dev/null 2>&1 || die "the Docker Compose plugin is not installed"
[ -f "$HOME_DIR/.env" ] && die "already installed in $HOME_DIR; use $HOME_DIR/scripts/upgrade.sh"
VERSION=$(sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' "$SRC/manifest.json" | head -n 1)
[ -n "$VERSION" ] || die "manifest.json not found: run install.sh from the extracted bundle"

echo "==> loading images"
for f in "$SRC"/images/*.tar; do docker load -i "$f"; done
echo "==> verifying the bundle"
docker run --rm --network none --user 0:0 --entrypoint /usr/local/bin/licserver -v "$SRC:/bundle:ro" "taktaplus-lic:$VERSION" verify-bundle-dir /bundle \
	|| die "the bundle failed verification"

URL=${1:-}
if [ -z "$URL" ]; then
	printf 'public URL of the license server (https://...): '
	read -r URL
fi
[ -n "$URL" ] || die "the public URL is required"

echo "==> installing into $HOME_DIR"
mkdir -p "$HOME_DIR/scripts" "$HOME_DIR/data/keys"
cp "$SRC/docker-compose.yml" "$HOME_DIR/"
cp "$SRC"/scripts/*.sh "$HOME_DIR/scripts/"
chmod 755 "$HOME_DIR"/scripts/*.sh
cd "$HOME_DIR"
umask 077
cat > .env <<ENV
TPLIC_HOME=$HOME_DIR
TPLIC_VERSION=$VERSION
TPLIC_MASTER_KEY=$(head -c 32 /dev/urandom | base64)
TPLIC_DB_PASSWORD=$(head -c 32 /dev/urandom | base64 | tr -d '/+=\n' | head -c 32)
TPLIC_PUBLIC_URL=$URL
TPLIC_HTTP_PORT=8080
TPLIC_LICENSE_KEY_FILE=
TPLIC_LICENSE_KEY_PASSPHRASE=
TPLIC_PACKAGE_KEY_FILE=
TPLIC_PACKAGE_KEY_PASSPHRASE=
ENV
umask 022
chown -R 65532:65532 data
chmod 700 data

echo "==> starting"
docker compose up -d
wait_healthy 60 || die "the license server did not start; see: docker compose -f $HOME_DIR/docker-compose.yml logs licserver"
cat <<MSG

The license server $VERSION is running on port 8080 (put a TLS proxy for $URL in front).
Next:
  1. Copy the encrypted signing keys (lictool keygen) into $HOME_DIR/data/keys/,
     set TPLIC_LICENSE_KEY_FILE=/var/lib/taktaplus-lic/keys/<kid>.key (and the
     package key) with their passphrases in $HOME_DIR/.env, then:
     cd $HOME_DIR && docker compose up -d
  2. Create the first owner:
     cd $HOME_DIR && docker compose exec licserver licserver create-admin -email you@example.com
Keep $HOME_DIR/.env safe: it holds the master key and key passphrases.
MSG
