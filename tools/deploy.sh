#!/bin/sh
# Build the Kindle binary and copy the app onto a USB-mounted Kindle.
set -e
cd "$(dirname "$0")/.."
KINDLE=${KINDLE:-/Volumes/Kindle}
[ -d "$KINDLE/documents" ] || { echo "Kindle not mounted at $KINDLE"; exit 1; }
GO=${GO:-/opt/homebrew/opt/go@1.23/bin/go}
(cd client && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 "$GO" build -trimpath -ldflags="-s -w" -o ../build/kcw-armv7 ./cmd/kcw)
APP="$KINDLE/extensions/calibre-wireless"
mkdir -p "$APP/bin"
cp build/kcw-armv7 "$APP/bin/kcw"
cp scriptlets/extension/run.sh "$APP/"
cp scriptlets/extension/config.default.sh "$APP/"   # config.sh (user settings) is left alone
[ -f scriptlets/extension/icon.png ] && cp scriptlets/extension/icon.png "$APP/"
cp "scriptlets/Calibre Wireless.sh" "$KINDLE/documents/"
dot_clean -m "$KINDLE" 2>/dev/null || true
echo "Deployed to $APP"
