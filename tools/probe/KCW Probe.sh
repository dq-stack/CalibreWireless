#!/bin/sh
# Name: KCW Probe
# DontUseFBInk
#
# Read-only probe for the calibre wireless client (round 2: PDF covers).
# Changes nothing on the Kindle; only writes /mnt/us/kcw-probe/report2.txt.

OUT=/mnt/us/kcw-probe
mkdir -p "$OUT"
R="$OUT/report2.txt"
DB=/var/local/cc.db

q() {
    # cc.db is often busy; retry a few times.
    for i in 1 2 3 4 5; do
        sqlite3 -header "$DB" "$1" 2>&1 && return
        sleep 1
    done
}

{
    echo "KCW probe 2 $(date)"

    echo; echo "== non-MOBI documents in cc.db"
    q "SELECT p_uuid, p_location, p_cdeKey, p_cdeType, p_mimeType, p_thumbnail, p_cover, p_percentFinished, p_readState, p_isProcessed, p_originType FROM Entries WHERE p_type='Entry:Item' AND p_location LIKE '/mnt/us/documents/%' AND p_location NOT LIKE '%.azw3' AND p_location NOT LIKE '%.azw' AND p_location NOT LIKE '%.mobi' AND p_location NOT LIKE '%.sh';"

    echo; echo "== thumbnails for those documents"
    for k in $(sqlite3 "$DB" "SELECT p_cdeKey FROM Entries WHERE p_type='Entry:Item' AND p_location LIKE '/mnt/us/documents/%' AND p_location NOT LIKE '%.azw3' AND p_location NOT LIKE '%.azw' AND p_location NOT LIKE '%.mobi' AND p_location NOT LIKE '%.sh';" 2>/dev/null); do
        echo "key $k:"
        ls -la /mnt/us/system/thumbnails/ /var/local/*thumb* 2>/dev/null | grep -F -- "$k"
    done

    echo; echo "== one EBOK row for comparison"
    q "SELECT p_cdeKey, p_cdeType, p_mimeType, p_thumbnail, p_cover, p_originType FROM Entries WHERE p_type='Entry:Item' AND p_location LIKE '/mnt/us/documents/%.azw3' LIMIT 1;"

    echo; echo "== other thumbnail folders"
    ls -la /var/local/ 2>&1 | grep -i "thumb\|cover"
    find / -maxdepth 4 -type d \( -iname '*thumbnail*' -o -iname '*cover*' \) 2>/dev/null | grep -v '^/proc' | head -20
} > "$R" 2>&1

eips 2 30 "KCW probe 2 done: kcw-probe/report2.txt" >/dev/null 2>&1
