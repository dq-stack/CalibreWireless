#!/bin/sh
# Name: Calibre Wireless
# DontUseFBInk
#
# Tap to connect to calibre over Wi-Fi; tap again to disconnect.
# Everything else lives in /mnt/us/extensions/calibre-wireless/.

APP=/mnt/us/extensions/calibre-wireless
RUN=/tmp/calibre-wireless

# Already running: ask the client to stop. The runner restores Wi-Fi, sleep
# and the firewall when the client exits.
if [ -f "$RUN/pid" ] && kill -0 "$(cat "$RUN/pid")" 2>/dev/null; then
    kill -TERM "$(cat "$RUN/pid")"
    exit 0
fi

if [ ! -x "$APP/bin/kcw" ]; then
    eips 0 38 " Calibre Wireless: $APP/bin/kcw is missing" >/dev/null 2>&1
    exit 1
fi

# Run from /tmp: USB drive mode can kill processes that use /mnt/us.
rm -rf "$RUN"
mkdir -p "$RUN"
cp "$APP/run.sh" "$APP/bin/"* "$RUN/"
chmod +x "$RUN/"*
cd /
nohup sh "$RUN/run.sh" >/dev/null 2>&1 &
exit 0
