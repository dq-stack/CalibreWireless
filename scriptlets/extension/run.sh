#!/bin/sh
# Calibre Wireless runner. Started from /tmp/calibre-wireless by the scriptlet.
# Turns Wi-Fi on, opens one firewall port for calibre's discovery reply, keeps
# the Kindle awake, runs the client, then puts everything back.

APP=/mnt/us/extensions/calibre-wireless
RUN=/tmp/calibre-wireless
LOG=$RUN/kcw.log
UDP_PORT=8134

# User settings (see config.sh for the list).
PASSWORD=""
HOST=""
PORT=9090
NAME="Kindle"
FORMATS="azw3,mobi,azw,kfx,pdf,txt"
READ_COL=""
READ_DATE_COL=""
IDLE="30m"
[ -f "$APP/config.sh" ] && . "$APP/config.sh"

log() { echo "$(date '+%Y/%m/%d %H:%M:%S') run: $*" >> "$LOG"; }

# Status display: our FBInk, CoverSleep's FBInk, or eips.
SHOW=""
if [ -x "$RUN/fbink" ]; then
    SHOW="$RUN/fbink"
elif [ -x /mnt/us/extensions/custom-screensaver/bin/fbink ]; then
    cp /mnt/us/extensions/custom-screensaver/bin/fbink "$RUN/fbink" && SHOW="$RUN/fbink"
fi
[ -z "$SHOW" ] && SHOW="$(command -v eips)"
show() {
    log "STATUS $1"
    case "$SHOW" in
        *eips) eips 0 38 "$(printf '%-60.60s' " $1")" >/dev/null 2>&1 ;;
        ?*) "$SHOW" -q -m -p -y -4 "$1" >/dev/null 2>&1 ;;
    esac
}

WIFI_WAS="$(lipc-get-prop com.lab126.cmd wirelessEnable 2>/dev/null)"
FW_ADDED=0
W1=""
W2=""

cleanup() {
    trap '' INT TERM
    [ -n "$W1" ] && kill "$W1" 2>/dev/null
    [ -n "$W2" ] && kill "$W2" 2>/dev/null
    if [ "$FW_ADDED" = 1 ]; then
        iptables -D INPUT -i wlan0 -p udp --dport "$UDP_PORT" -j ACCEPT 2>/dev/null
        log "firewall rule removed"
    fi
    lipc-set-prop com.lab126.powerd preventScreenSaver 0 2>/dev/null
    if [ "$WIFI_WAS" = 0 ]; then
        lipc-set-prop com.lab126.cmd wirelessEnable 0 2>/dev/null
        log "Wi-Fi turned back off"
    fi
    rm -f "$RUN/pid"
    log "done"
    # Keep a copy of the log where it can be read over USB (fails harmlessly
    # if USB drive mode has already taken /mnt/us).
    if [ -d "$APP" ]; then
        { [ -f "$APP/kcw.log" ] && tail -n 3000 "$APP/kcw.log"; cat "$LOG"; } > "$APP/kcw.log.new" 2>/dev/null \
            && mv "$APP/kcw.log.new" "$APP/kcw.log"
    fi
}
trap cleanup EXIT
trap 'exit 1' INT TERM

log "start (Wi-Fi was ${WIFI_WAS:-unknown})"

# 1. Wi-Fi
if [ "$WIFI_WAS" = 0 ]; then
    show "Turning on Wi-Fi..."
    lipc-set-prop com.lab126.cmd wirelessEnable 1
fi
i=0
while [ "$(lipc-get-prop com.lab126.wifid cmState 2>/dev/null)" != "CONNECTED" ]; do
    i=$((i + 1))
    if [ $i -gt 45 ]; then
        show "Wi-Fi didn't connect. Check Settings > Wi-Fi Networks."
        exit 1
    fi
    sleep 1
done
log "Wi-Fi connected after ${i}s"

# 2. Firewall: let calibre's discovery reply in on our fixed port only
# (KOReader does the same with this port). Drop any copy left by a crash first.
while iptables -D INPUT -i wlan0 -p udp --dport "$UDP_PORT" -j ACCEPT 2>/dev/null; do :; done
if iptables -A INPUT -i wlan0 -p udp --dport "$UDP_PORT" -j ACCEPT; then
    FW_ADDED=1
    log "firewall rule added (udp $UDP_PORT)"
else
    log "couldn't add firewall rule; discovery may fail"
fi

# 3. Stay awake while connected.
lipc-set-prop com.lab126.powerd preventScreenSaver 1

# 4. Client.
set -- -root /mnt/us -log "$LOG" -status "$SHOW" -udp-port "$UDP_PORT" \
    -name "$NAME" -formats "$FORMATS" -idle "$IDLE" -port "$PORT"
[ -n "$HOST" ] && set -- "$@" -host "$HOST"
[ -n "$PASSWORD" ] && set -- "$@" -password "$PASSWORD"
[ -n "$READ_COL" ] && set -- "$@" -read-col "$READ_COL"
[ -n "$READ_DATE_COL" ] && set -- "$@" -read-date-col "$READ_DATE_COL"
"$RUN/kcw" "$@" 2>/dev/null &
KCW=$!
echo "$KCW" > "$RUN/pid"

# 5. Stop the client if USB drive mode starts (same events as CoverSleep and
# InkSights). lipc-wait-event exits when the event fires.
lipc-wait-event com.lab126.hal usbConfigured >/dev/null 2>&1 &
W1=$!
lipc-wait-event com.lab126.volumd userstoreIsLikelyToUnMount,userstoreUnAvailable >/dev/null 2>&1 &
W2=$!
sleep 1
# A watcher that died at once couldn't subscribe; don't mistake that for USB.
kill -0 "$W1" 2>/dev/null || { log "usb watcher 1 unavailable"; W1=""; }
kill -0 "$W2" 2>/dev/null || { log "usb watcher 2 unavailable"; W2=""; }

while kill -0 "$KCW" 2>/dev/null; do
    if { [ -n "$W1" ] && ! kill -0 "$W1" 2>/dev/null; } || { [ -n "$W2" ] && ! kill -0 "$W2" 2>/dev/null; }; then
        log "USB connected, stopping"
        W1=""
        W2=""
        kill -TERM "$KCW" 2>/dev/null
        break
    fi
    sleep 1
done
wait "$KCW"
log "client exited ($?)"
