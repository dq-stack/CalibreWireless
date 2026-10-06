# Protocol notes

What the client has to speak, and how it fits into the stock Kindle. Sources are listed per
section. **Bold "VERIFY"** marks anything not yet confirmed on a real device.

Sources read (October 2026):
- calibre `src/calibre/devices/smart_device_app/driver.py` (calibre 9.x, GPLv3)
- calibre `src/calibre/devices/kindle/driver.py` (USB Kindle driver: thumbnails, cover-bug cache)
- KOReader `plugins/calibre.koplugin/wireless.lua`, `metadata.lua` (AGPL-3.0; read as reference only, no code copied)
- sandman7920/ccsync `kindle/commander.go`, `kindle/entries.go` (MIT)
- InkSights `collect.py` (our own; cc.db progress query)
- Dan's Kindle over USB (PW2, FW 5.12.2.2), read-only

## 1. Discovery (UDP)

calibre listens on UDP ports **54982, 48123, 39001, 44044, 59678** once "Start wireless
device connection" is on. The client sends any datagram (KOReader sends `hello`) to
`255.255.255.255:<port>` and waits for a reply:

```
calibre wireless device client (on <hostname>);<content_server_port>,<smart_device_port>
```

Regex: `calibre wireless device client \(on (.*)\);(\d*),(\d+)$`. The reply's source address is
calibre's IP; the last number is the TCP port (9090 unless the user changed it).
Try each port in turn with a ~3 s timeout. Fallback: let the user set a fixed `host:port` in
the config file (needed on networks that drop broadcasts).

calibre also publishes mDNS (`_calibresmartdeviceapp._tcp`); we don't need it.

## 2. Framing (TCP)

The client opens the TCP connection; calibre then drives everything and the client answers.

Every JSON message: `<decimal length><json>`, where json is `[opcode, {args}]` and the length is
the byte length of the JSON text. No separator. Example: `34[0, {"key0": 1, "key1": 2}]`.
Reader: read digits until `[`, then read exactly `length` bytes counted from the `[`.

Book contents are sent as **raw bytes immediately after** the SEND_BOOK exchange (no framing),
exactly `length` bytes, in chunks of at most `maxBookContentPacketLen`. Any bytes after that
belong to the next JSON message.

Opcodes:

| op | name | direction / notes |
|---|---|---|
| 0 | OK | reply |
| 1 | SET_CALIBRE_DEVICE_INFO | calibre → driveinfo dict; reply OK {} and save it |
| 2 | SET_CALIBRE_DEVICE_NAME | rename; reply OK {} |
| 3 | GET_DEVICE_INFORMATION | reply OK {device_info:{device_store_uuid, device_name}, version, device_version} |
| 4 | TOTAL_SPACE | reply OK {total_space_on_device} |
| 5 | FREE_SPACE | reply OK {free_space_on_device} |
| 6 | GET_BOOK_COUNT | see §5 |
| 7 | SEND_BOOKLISTS | no reply; followed by `count` SEND_BOOK_METADATA messages |
| 8 | SEND_BOOK | see §6 |
| 9 | GET_INITIALIZATION_INFO | handshake, see §3 |
| 11 | BOOK_DONE | unused by current calibre |
| 12 | NOOP | keep-alive; also carries `ejecting`, `count`, `priKey` (see §5, §8) |
| 13 | DELETE_BOOK | see §7 |
| 14 | GET_BOOK_FILE_SEGMENT | calibre pulls a file: reply OK {fileLength} then raw bytes |
| 15 | GET_BOOK_METADATA | unused by current calibre |
| 16 | SEND_BOOK_METADATA | no reply |
| 17 | DISPLAY_MESSAGE | messageKind 1 = wrong password, 2 = update needed, 3 = toast |
| 18 | CALIBRE_BUSY | another device is connected |
| 19 | SET_LIBRARY_INFO | reply OK {} |
| 20 | ERROR | client → calibre {message}, e.g. refusing a book |
| 21, 22 | GET_COLLECTIONS, UPDATE_COLLECTIONS | defined, but calibre's driver never sends them |

## 3. Handshake

1. calibre → `GET_INITIALIZATION_INFO` with `serverProtocolVersion` (1), `validExtensions`,
   `passwordChallenge`, `currentLibraryName`, `currentLibraryUUID`, `calibre_version` [x,y,z],
   `canSupportUpdateBooks`, `canSupportLpathChanges`, date formats.
2. client → `OK` with:

| field | our value | why |
|---|---|---|
| versionOK | true | required |
| maxBookContentPacketLen | 4096+ (try 65536) | must be > 0 |
| acceptedExtensions | ["azw3","mobi","azw","kfx","pdf","txt"] (configurable) | non-empty list required; drives calibre's format choice |
| canStreamBooks, canStreamMetadata, canReceiveBookBinary, canDeleteMultipleBooks | true | all four required or calibre says "app too old" |
| canSendOkToSendbook | true | lets us reply to SEND_BOOK and **change the lpath** (§6) |
| canUseCachedMetadata, cacheUsesLpaths | true | short book lists on reconnect |
| canAcceptLibraryInfo | true | |
| coverHeight | 500 | what calibre's USB Kindle driver uses (KOReader uses 240) |
| isReadSyncCol, isReadDateSyncCol | from config, e.g. "#read", "#last_read" | §9 |
| setTempMarkWhenReadInfoSynced | false (configurable) | marks synced books in calibre |
| deviceKind / deviceName | "Kindle (PW2)" etc. | shown in calibre |
| appName | "KindleCalibreWireless" (name TBD) | must not be "CalibreCompanion" |
| ccVersionNumber | our version | |
| extensionPathLengths | {ext: 37} per ext (KOReader's value) | path length budget |
| useUuidFileNames | false | |
| passwordHash | sha1_hex(password + challenge) or "" | only checked if calibre has a password |

Password: `hex(SHA1(utf8(password) || utf8(challenge)))`. If wrong, calibre sends
DISPLAY_MESSAGE kind 1 then closes; show "wrong password" on screen.

3. calibre → GET_DEVICE_INFORMATION → we answer with the saved driveinfo's `device_store_uuid`.
4. calibre → SET_CALIBRE_DEVICE_INFO (driveinfo dict) → save, reply OK.
5. calibre → SET_LIBRARY_INFO, FREE_SPACE, TOTAL_SPACE, GET_BOOK_COUNT …, then SEND_BOOKLISTS.

calibre keeps a 5-failed-attempts-per-IP lockout, so don't hammer reconnects.

## 4. Storage layout: share calibre's USB files

Confirmed on Dan's Kindle: calibre's USB driver writes `/mnt/us/metadata.calibre` (JSON list,
40 books, lpaths like `documents/Author/Title.azw3`) and `/mnt/us/driveinfo.calibre`.
**Design decision:** the client's root is `/mnt/us` and it reads and writes those same two
files. Books sent over USB and over Wi-Fi are then one list, and calibre recognises USB-sent
books when connecting wirelessly (otherwise it treats them as unknown and pulls each file over
Wi-Fi to read its metadata, see `_new_book_` in §5).

Rules:
- Every lpath we accept must start with `documents/` (the only folder the Kindle indexes).
  calibre's wireless template has no such prefix, so on SEND_BOOK we reply with
  `lpath = "documents/" + lpath` (allowed because we set canSendOkToSendbook and calibre sends
  canSupportLpathChanges).
- Refuse unsafe lpaths (`..`, absolute paths, symlink escapes), as KOReader does.
- Write metadata.calibre atomically (temp file + rename) and keep calibre's full per-book
  metadata so the USB driver still reads it. Drop `thumbnail` payloads from it to keep it small.
- driveinfo: keep `device_store_uuid` stable. USB calibre rewrites `prefix`; harmless.

## 5. Book list (GET_BOOK_COUNT)

calibre → `{canStream, canScan, willUseCachedMetadata, supportsSync, canSupportBookFormatSync}`.

client → `OK {count, willStream: true, willScan: true}` then `count` × `OK {...}`, one per book:
- With the cache (willUseCachedMetadata true): send `{priKey, uuid, lpath, last_modified,
  extension, _is_read_, _last_read_date_, _sync_type_}`.
  calibre then sends `NOOP {count: n}` followed by n × `NOOP {priKey}`, and after **each**
  priKey we send `OK <full book metadata>` (no reply to the `count` NOOP).
- Without the cache: send full metadata for every book.

A book we send without calibre's metadata (a file found on disk that isn't in
metadata.calibre) must carry `_new_book_: true`; calibre will then download the file with
GET_BOOK_FILE_SEGMENT to read its metadata. Avoid this for big libraries; only do it for
files under `documents/` with a calibre-supported extension (**decide**: maybe skip
non-calibre books entirely).

## 6. Receiving a book (SEND_BOOK)

calibre → `{lpath, length, metadata, thisBook, totalBooks, willStreamBooks, willStreamBinary,
wantsSendOkToSendbook, canSupportLpathChanges}`.

client:
1. Validate lpath and free space (`length` + 128 KB margin). If bad → `ERROR {message}`.
2. Reply `OK {lpath: <final lpath>}`.
3. Read exactly `length` raw bytes into `<path>.part`, fsync, rename into place.
4. Add/replace the book in metadata.calibre (keyed by lpath).
5. Write the cover thumbnail (§10).

Then calibre (in sync_booklists) sends SEND_BOOKLISTS + SEND_BOOK_METADATA for changed books.

## 7. Delete (DELETE_BOOK)

calibre → `{lpaths: [...]}`. Reply `OK {}` once, then for each lpath delete the file and its
sidecars, and reply `OK {uuid}` per lpath (calibre reads one reply per path).
Sidecars to delete (same as the USB driver's DELETE_EXTS): `.mbp .tan .pdr .ea .apnx .phl`,
the `<name>.sdr/` folder, and the thumbnail in `system/thumbnails/` + `amazon-cover-bug/`.
**Decide:** deleting `.sdr` loses reading position and InkSights timing data. calibre's USB
driver does delete `.sdr`; InkSights' backups cover it. Default = match USB.

## 8. Keep-alive and disconnect

calibre sends `NOOP {}` periodically; reply `OK {}`. `NOOP {ejecting: true}` = calibre is
disconnecting: reply `OK {}` and close. calibre's auto-disconnect is 30 min of inactivity
(if the user enables it). The client should also time out on its own (configurable, default
30 min with no traffic) so Wi-Fi isn't left on.

## 9. Read status back to calibre

Only two values sync, via calibre's synchronize_with_db:
- `_is_read_` → a calibre **Yes/No** custom column named in `isReadSyncCol` (e.g. `#read`)
- `_last_read_date_` → a calibre **Date** custom column named in `isReadDateSyncCol`

calibre checks the column types and shows an error if they're wrong. **Progress % cannot be
synced through this protocol** (that stays with InkSights / a future plugin).

How calibre decides who wins (standard case, `_sync_type_` unset): it compares our `_is_read_`
with the column value **in the metadata calibre last sent us** (`user_metadata["#read"]`).
If different, the Kindle value is written into calibre. So the client must:
- keep the user_metadata calibre sends (SEND_BOOK / SEND_BOOK_METADATA) per book, and
- report `_is_read_` = the stored calibre value unless the Kindle state changed since then.

`_sync_type_ = 2` (first sync after the user sets up a column): device wins if not null,
otherwise calibre's value is pushed to the device. Use this the first time a column is
configured. `_sync_type_ = 3` means "metadata invented by the device", don't use.

Kindle side (cc.db, `/var/local/cc.db`, table `Entries`, `p_type='Entry:Item'`):
- `p_location` = full path (`/mnt/us/documents/...`), join key with lpath.
- `p_percentFinished` = real progress (InkSights finding: use this, not timer.model).
- `p_lastAccess` = last opened (epoch seconds) → `_last_read_date_` (ISO 8601 UTC).
- read state: FW 5.12 **has `p_readState`** (probe, 6 Oct). Seen values: empty (NULL) and 1.
  **VERIFY** the meaning (mark a book "as read" on the Kindle, re-probe). Fallback:
  `_is_read_ = percentFinished >= threshold` (config, default 98).
- Open read-only (`file:/var/local/cc.db?mode=ro`); the framework writes it constantly.
  The probe's `.tables` hit "database is locked" while the PRAGMA worked, so set a busy
  timeout and retry reads.

## 10. Covers and the Amazon cover bug

From calibre's USB Kindle driver:
- Thumbnail path: `/mnt/us/system/thumbnails/thumbnail_<cdeKey>_<cdeType>_portrait.jpg`.
  cdeKey = MOBI EXTH 113, cdeType = EXTH 501 (KFX: book key from the container).
  For calibre-made books the cdeKey is calibre's book UUID (seen on Dan's Kindle), but read
  EXTH anyway (Send-to-Kindle PDOC books and `mobi-asin` identifiers differ).
- Image: `metadata.thumbnail` = `[width, height, base64 JPEG]`, height = our coverHeight (500).
- **Amazon cover bug** (MobileRead t=329945): when online, the Kindle may replace sideloaded
  covers. calibre's workaround, which we copy exactly: also save each thumbnail to
  `/mnt/us/amazon-cover-bug/`, and on every connect copy back any thumbnail whose size differs
  or is missing. Dan's Kindle already has this folder (44 files) from USB use. The client does
  the restore on connect **and on disconnect** (Wi-Fi was on the whole time).
  The jailbreak does not include any protection against this.
- APNX (stretch): FW 5.12 only reads it inside `<book>.sdr/`. calibre's USB driver detects
  this by looking for `.sdr` folders. Generating APNX on the device is out of scope for now.

## 11. Collections (content catalog)

From ccsync (MIT), for later milestones:
- `POST http://127.0.0.1:9101/change`, header `AuthToken: <contents of /tmp/session_token>`
  if that file exists, body:
  `{"type":"ChangeRequest","id":1,"commands":[...]}`.
- Commands: `{"delete":{"uuid":...}}`; `{"insert":{"type":"Collection","uuid":U,
  "lastAccess":ts,"titles":[{"display":name}],"isVisibleInHome":1,"isArchived":1,
  "mimeType":"application/x-kindle-collection","collections":null,"collectionCount":null,
  "collectionDataSetName":U},"update":{"type":"Collection","uuid":U,"members":[book p_uuid...]}}`;
  if Entries has `p_collectionCount`: `{"update":{"type":"Entry:Item","uuid":book,"collectionCount":n}}`.
- Books are found in cc.db by `p_location` (ccsync matches by `p_cdeKey`, EBOK only).
- Existing collections: `Entries WHERE p_type='Collection'`; members: `Collections` table
  (`i_collection_uuid`, `i_member_uuid`).
- Source of collection names: calibre already sends `collections` (name → [lpaths]) in
  SEND_BOOKLISTS, built from the driver's "metadata fields to turn into collections" setting
  (default `series,tags`). So the user configures this in calibre, not on the Kindle.
- New books aren't in cc.db until the Kindle indexes them: after a transfer, poll cc.db for the
  new `p_location`s (e.g. every 2 s, up to 60 s) before sending the change request.
- Probe (FW 5.12.2.2): 127.0.0.1:9101 is listening, `/tmp/session_token` exists, Entries has
  `p_collectionCount`, 0 collections today. **VERIFY** ccsync's command format is accepted.

## 12. Kindle platform notes (from CoverSleep / InkSights)

- PW2: FW 5.12.2.2, soft-float (only /lib/ld-linux.so.3), kernel **3.0.35-lab126** (probe)
  → Go 1.23 (1.24+ needs kernel ≥ 3.2). Tools present: /usr/bin/sqlite3, lipc-*, eips, wget,
  curl, netstat. No fbink on the PATH. Static `CGO_ENABLED=0 GOARCH=arm GOARM=7` runs on soft- and hard-float firmware.
- USB drive mode can SIGKILL processes using /mnt/us: run from `/tmp`, and stop on
  `com.lab126.hal usbConfigured` / `com.lab126.volumd userstoreIsLikelyToUnMount`.
- Sleep: `lipc-set-prop com.lab126.powerd preventScreenSaver 1` while connected, `0` on exit
  (always, via trap).
- Wi-Fi: `com.lab126.cmd wirelessEnable` = 0 in airplane mode, `com.lab126.wifid cmState` = NA,
  and wlan0 doesn't exist while off. Turn on with `lipc-set-prop com.lab126.cmd wirelessEnable 1`,
  wait for cmState `CONNECTED` (**VERIFY**), and restore the previous state on exit.
- Firewall: INPUT policy DROP; UDP is accepted only for ESTABLISHED. The discovery reply
  comes from calibre's unicast IP to a datagram we sent to 255.255.255.255, so conntrack
  probably won't match it and the reply is **dropped**. Options: a temporary
  `iptables -I INPUT -p udp --sport <calibre port> -j ACCEPT` for the 5 ports, removed on exit
  (needs Dan's OK), or a fixed calibre address in the config. TCP to calibre is outbound, so
  it's fine.
- OTA: no update files in /mnt/us (probe).
- Status on screen: FBInk (`/mnt/us/extensions/custom-screensaver/bin/fbink` exists on Dan's
  Kindle; we should ship our own copy, or fall back to `eips`).
- Clock: Dan's Kindle now has its time set; other users' may show UTC. `_last_read_date_` is
  sent as UTC, so this only matters for display.
