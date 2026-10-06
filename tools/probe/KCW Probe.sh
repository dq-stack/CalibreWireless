#!/bin/sh
# Name: KCW Probe
# DontUseFBInk
#
# Read-only probe for the calibre wireless client (round 3: read state).
# Changes nothing on the Kindle; only writes /mnt/us/kcw-probe/report3.txt.

OUT=/mnt/us/kcw-probe
mkdir -p "$OUT"
R="$OUT/report3.txt"
{
    echo "KCW probe 3 $(date)"
    for i in 1 2 3 4 5; do
        sqlite3 -header /var/local/cc.db "SELECT p_titles_0_nominal AS title, p_percentFinished AS pct, p_readState AS readState, p_lastAccess AS lastAccess, datetime(p_lastAccess,'unixepoch') AS lastAccessUTC, p_isLatestItem AS latest FROM Entries WHERE p_type='Entry:Item' AND p_location LIKE '/mnt/us/documents/%' ORDER BY p_lastAccess DESC;" && break
        sleep 1
    done
} > "$R" 2>&1
eips 2 30 "KCW probe 3 done: kcw-probe/report3.txt" >/dev/null 2>&1
