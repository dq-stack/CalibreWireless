# Calibre Wireless settings. Edit config.sh (made from config.default.sh on the
# first run) over USB; changes apply on the next tap. Upgrades never touch it.
# Lines starting with # are ignored. Keep the quotes.

# Name calibre shows for this Kindle.
NAME="Kindle"

# Only if you set a password in calibre (Connect/share > Start wireless
# device connection > password field).
PASSWORD=""

# calibre's address, used only when the automatic search finds nothing
# (e.g. a network that blocks broadcasts). Example: HOST="192.168.1.3"
HOST=""
PORT=9090

# Formats this Kindle accepts, preferred first. calibre converts to the first
# one it can.
FORMATS="azw3,mobi,azw,kfx,pdf,txt"

# Read status back to calibre (optional). Create two custom columns in
# calibre (Preferences > Add your own columns): a Yes/No column and a Date
# column, then put their lookup names here with a #, e.g. READ_COL="#read".
# Changes you make in calibre are kept; the Kindle's value is only sent when
# it changes on the Kindle.
READ_COL=""
READ_DATE_COL=""

# A book counts as read when you use the Kindle's "Mark as Read". Set a
# number (e.g. 98) to also count books at or past that % as read.
READ_PERCENT=0

# Disconnect after this long with no transfers (e.g. 15m, 1h).
IDLE="30m"

# On-screen messages use FBInk if one is installed (e.g. by CoverSleep or
# KUAL), otherwise the Kindle's built-in eips. Set to "eips" to force eips.
STATUS_WITH=""
