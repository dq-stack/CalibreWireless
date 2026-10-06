#!/bin/sh
# Name: KCW Probe
# DontUseFBInk
#
# Read-only probe for the calibre wireless client. Changes nothing on the Kindle;
# only writes /mnt/us/kcw-probe/report.txt. Delete this file and that folder afterwards.

OUT=/mnt/us/kcw-probe
mkdir -p "$OUT"
R="$OUT/report.txt"

section() { echo; echo "== $1"; }

{
    echo "KCW probe $(date)"

    section "kernel / cpu"
    uname -a
    grep -m1 -i "model name\|Processor" /proc/cpuinfo
    grep -m1 -i "Features" /proc/cpuinfo
    ls /lib/ld-linux-armhf.so.3 /lib/ld-linux.so.3 2>&1

    section "firmware"
    cat /etc/prettyversion.txt 2>/dev/null
    cat /etc/version.txt 2>/dev/null | head -3

    section "tools"
    for t in sqlite3 lipc-get-prop lipc-set-prop lipc-wait-event eips fbink netstat ss wget curl; do
        printf '%s: ' "$t"; which "$t" 2>/dev/null || echo "-"
    done
    ls -l /mnt/us/extensions/custom-screensaver/bin/fbink 2>&1

    section "free space"
    df -k /mnt/us /tmp

    section "cc.db Entries columns"
    if which sqlite3 >/dev/null 2>&1; then
        sqlite3 "file:/var/local/cc.db?mode=ro" "PRAGMA table_info(Entries);" 2>&1 \
            || sqlite3 /var/local/cc.db "PRAGMA table_info(Entries);" 2>&1
        section "cc.db tables"
        sqlite3 /var/local/cc.db ".tables" 2>&1
        section "cc.db sample books (read state candidates)"
        sqlite3 -header /var/local/cc.db "SELECT * FROM Entries WHERE p_type='Entry:Item' AND p_location LIKE '/mnt/us/documents/%' AND p_percentFinished IS NOT NULL LIMIT 3;" 2>&1
        section "cc.db collections"
        sqlite3 /var/local/cc.db "SELECT count(*) FROM Entries WHERE p_type='Collection';" 2>&1
    else
        echo "no sqlite3"
    fi

    section "content catalog (port 9101)"
    (netstat -tln 2>/dev/null || cat /proc/net/tcp) | grep -i ":9101\|:238D" || echo "9101 not seen"
    if [ -e /tmp/session_token ]; then echo "/tmp/session_token exists"; else echo "/tmp/session_token missing"; fi

    section "wifi lipc"
    lipc-get-prop com.lab126.cmd wirelessEnable 2>&1
    lipc-get-prop com.lab126.wifid cmState 2>&1
    lipc-get-prop com.lab126.wifid signalStrength 2>&1
    ifconfig wlan0 2>&1 | head -3

    section "power lipc"
    lipc-get-prop com.lab126.powerd preventScreenSaver 2>&1
    lipc-get-prop com.lab126.powerd state 2>&1

    section "firewall"
    iptables -L -n 2>&1 | head -30

    section "OTA"
    ls -la /mnt/us/ | grep -i "update\|\.bin" || echo "no update files in /mnt/us"
    ls -la /var/local/ota 2>&1 | head
} > "$R" 2>&1

eips 2 30 "KCW probe done: kcw-probe/report.txt" >/dev/null 2>&1
