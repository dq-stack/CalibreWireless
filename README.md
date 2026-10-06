# Calibre Wireless

Send books from calibre to a **jailbroken Kindle over Wi-Fi**, on the **stock Kindle reader**.
No KOReader, no USB cable, no SSH, no extra library manager. Tap one app in your Kindle
library and it appears in calibre as a wireless device, just like KOReader does.

It also does things other wireless clients don't:

- **Read status back to calibre.** Books you mark as read on the Kindle, and the date you last
  read them, go into calibre columns.
- **Covers that stick.** Writes calibre's cover for each book and puts it back if the Kindle
  swaps in Amazon's blank "No image available" while online (the "Amazon cover bug").
- **Page numbers.** Writes the page-number file (APNX) calibre's USB driver makes, so books
  show pages instead of Loc.
- **One library with USB.** Uses the same book list calibre's USB connection keeps on the
  Kindle, so books sent either way are recognised both ways.

## Requirements

- A **jailbroken** Kindle that can run **Scriptlets** (shell scripts started from the
  library, e.g. hackerdude's shell_integration).
- **calibre** on a computer on the **same Wi-Fi network** as the Kindle.
- Tested on a **Kindle Paperwhite 2 (6th gen), firmware 5.12.2.2**. Other models on firmware
  5.x should work but haven't been tested yet. Reports welcome!

## Install

1. Download `calibre-wireless-X.Y.Z.zip` from
   [Releases → Assets](../../releases/latest) (not the source code zip).
2. Plug the Kindle into your computer and **unzip it onto the Kindle's root** (the drive itself),
   so you get `extensions/calibre-wireless/` and `documents/Calibre Wireless.sh`.
3. Eject the Kindle. **Calibre Wireless** appears in your library.

Upgrading: unzip the new release over the old one. Your settings (`config.sh`) are kept.

## Use

1. In calibre: **Connect/share → Start wireless device connection**.
   Tip: tick *Automatically allow connections at startup* in that dialog so you don't have to
   do this every time calibre starts.
2. On the Kindle: tap **Calibre Wireless**. It turns on Wi-Fi if needed, finds calibre and
   connects. Messages appear at the bottom of the screen.
3. Use calibre as with any device: send books, delete books from the Device view.
4. **Tap Calibre Wireless again to disconnect.** It also stops on its own when you eject it in
   calibre, after 30 minutes without transfers, or when you plug in USB.

When it stops it puts everything back: Wi-Fi off again if it was off, normal sleep, and the
firewall rule removed (see below).

## Setting up calibre (once)

**File names (avoids duplicate books).** calibre's wireless connection names files differently
from its USB connection, so a book sent once each way would end up on the Kindle twice. Make
them match: *Preferences → Plugins → Device interface plugins → Smart Device App Interface →
Customize plugin*, and set **Save template** to:

```
{author_sort}/{title} - {authors}
```

**Read status (optional).** Create two columns in calibre (*Preferences → Add your own
columns*):

| Column | Type | Example lookup name |
|---|---|---|
| Read | Yes/No | `read` |
| Last read | Date | `last_read` |

Then set them in the Kindle's `extensions/calibre-wireless/config.sh` (see Settings):

```sh
READ_COL="#read"
READ_DATE_COL="#last_read"
```

- **Read** is the Kindle's own *Mark as Read* (long-press a book). Optionally also count books
  past a percentage as read with `READ_PERCENT=98`.
- **Last read** is when you last opened the book on the Kindle.
- It goes one way, Kindle → calibre. Things you change in calibre are kept: the Kindle's value is
  only sent when it changes on the Kindle.
- These columns show in calibre's **library** view (calibre's device view never shows custom
  columns).

## Settings

`extensions/calibre-wireless/config.sh` is created from `config.default.sh` the first time
you tap the app. Edit it over USB (or copy `config.default.sh` to `config.sh` yourself first).

| Setting | Default | What it does |
|---|---|---|
| `NAME` | `Kindle` | Name calibre shows |
| `PASSWORD` | empty | Only if you set a password in calibre's wireless dialog |
| `HOST`, `PORT` | empty, `9090` | calibre's address, used only if the automatic search fails |
| `FORMATS` | `azw3,mobi,azw,kfx,pdf,txt` | Formats accepted, preferred first |
| `READ_COL`, `READ_DATE_COL` | empty | calibre columns for read status (above) |
| `READ_PERCENT` | `0` | Also count books at/after this % as read |
| `IDLE` | `30m` | Disconnect after this long without transfers |
| `STATUS_WITH` | empty | `eips` forces the Kindle's built-in text display instead of FBInk |

## Troubleshooting

**"calibre still has a USB Kindle connected" / calibre says busy.** calibre allows one device
at a time, and it keeps hold of a USB Kindle even after you unplug it. Eject the Kindle in
calibre before unplugging, or quit calibre fully (**⌘Q** on a Mac; closing the window isn't
enough), reopen it and start the wireless connection again. Books you "send" while calibre is
stuck on the USB device are queued and arrive the next time you plug in.

**"calibre not found".** The Kindle and computer must be on the same network. A phone hotspot,
guest Wi-Fi or a second router won't work. The message shows the Kindle's address (e.g.
`from 192.168.1.20`). If the computer is on the same network but broadcasts are blocked, set
`HOST` to the computer's IP address in `config.sh`.

**The cover shows "No image available" for a moment.** That's Amazon replacing it while the
Kindle is online. The app puts the real cover back within a second or two. The library shows it
again on its next redraw, or when you disconnect.

**No page numbers on books that were already on the Kindle.** The app adds page numbers to
them, but if you opened the book before, the Kindle has cached it: restart the Kindle once.
Books sent with the app get page numbers straight away.

**PDFs have no cover.** The Kindle doesn't use cover files for PDFs (personal documents). This
is the same with calibre over USB.

**Logs.** `extensions/calibre-wireless/kcw.log` (copied there when the app stops).

## What it changes on your Kindle

- **While running:** turns Wi-Fi on (and back off afterwards if it was off), stops the Kindle
  sleeping, and adds one firewall rule (`iptables -A INPUT -i wlan0 -p udp --dport 8134 -j
  ACCEPT`, the same one KOReader uses) so calibre's reply to the search gets in. All three are
  undone when it stops, and a restart clears them anyway.
- **Files:** books in `documents/`, calibre's `metadata.calibre` and `driveinfo.calibre` (the
  same files calibre's USB driver writes), covers in `system/thumbnails/` and their backup in
  `amazon-cover-bug/` (calibre's own backup folder), page numbers in each book's `.sdr` folder,
  and its own files in `extensions/calibre-wireless/`.
- **The Kindle's database (`cc.db`)** is only ever **read** (for read status), never written.

The app runs from `/tmp` while connected so that plugging in USB can't kill it halfway.

## Uninstall

Delete `documents/Calibre Wireless.sh` and the `extensions/calibre-wireless/` folder.

## How it works / building

- `client/`: the client, in Go. Built as one static ARMv7 binary (`CGO_ENABLED=0 GOARCH=arm
  GOARM=7`) that runs on both soft-float and hard-float Kindle firmware. It needs **Go 1.23**:
  Go 1.24+ requires Linux kernel 3.2 and older Kindles run 3.0.x.
- `scriptlets/`: the library scriptlet and the launcher (`run.sh`).
- `docs/PROTOCOL.md`: calibre's Smart Device protocol and the Kindle details, including notes
  for features not built (collections).
- `tools/package.sh VERSION` builds the release zip. `tools/deploy.sh` copies a dev build to a
  USB-mounted Kindle. The same binary runs on a computer as a test client:
  `go run ./cmd/kcw -root /path/to/folder-with-documents`.

Not included (contributions welcome): Kindle collections from calibre tags/series (see
`docs/PROTOCOL.md`), KFX covers, PDF covers.

## Credits

- [calibre](https://calibre-ebook.com) (GPLv3) by Kovid Goyal and contributors: the Smart Device
  protocol, the Amazon cover-bug fix and the APNX page-number method this app follows.
- [KOReader](https://github.com/koreader/koreader)'s calibre plugin (AGPLv3): used as a reference
  for how a client behaves; no code copied.
- [ccsync](https://github.com/sandman7920/ccsync) (MIT) by sandman7920: research on the
  Kindle's content catalog.
- [FBInk](https://github.com/NiLuJe/FBInk) by NiLuJe: used for on-screen messages when it's
  installed.

## License

GPLv3. See [LICENSE](LICENSE).
