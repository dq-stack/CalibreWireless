#!/bin/sh
# Build the release zip: unzip it onto the Kindle's root (the USB drive).
#   tools/package.sh 1.0.0   ->  release/calibre-wireless-1.0.0.zip
set -e
cd "$(dirname "$0")/.."
VERSION=${1:?usage: tools/package.sh VERSION}
GO=${GO:-go}
command -v "$GO" >/dev/null 2>&1 || GO=/opt/homebrew/opt/go@1.23/bin/go

STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT
APP="$STAGE/extensions/calibre-wireless"
mkdir -p "$APP/bin" "$STAGE/documents" release

(cd client && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 "$GO" build -trimpath \
    -ldflags="-s -w -X main.version=$VERSION" -o "$APP/bin/kcw" ./cmd/kcw)
cp scriptlets/extension/run.sh scriptlets/extension/config.default.sh scriptlets/extension/icon.png "$APP/"
cp LICENSE "$APP/LICENSE"
echo "$VERSION" > "$APP/VERSION"
cp "scriptlets/Calibre Wireless.sh" "$STAGE/documents/"
chmod 755 "$APP/run.sh" "$APP/bin/kcw" "$STAGE/documents/Calibre Wireless.sh"

OUT="release/calibre-wireless-$VERSION.zip"
rm -f "$OUT"
(cd "$STAGE" && zip -qrX "$OLDPWD/$OUT" extensions documents)
echo "$OUT"
unzip -l "$OUT"
