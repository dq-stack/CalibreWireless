"""Draws the Calibre Wireless library icon (original artwork, GPLv3 with the
project): an open book with Wi-Fi waves rising from the spine.
Output: 450x600 greyscale PNG, the Kindle scriptlet icon size.
Usage: python3 draw_icon.py out.png"""
import math
import sys

from PIL import Image, ImageDraw

W, H, S = 450, 600, 4          # final size, supersampling factor
STROKE = 15                     # line width at final size
img = Image.new("L", (W * S, H * S), 255)
d = ImageDraw.Draw(img)
r = STROKE * S / 2


def dot(x, y, rad=r):
    d.ellipse((x * S - rad, y * S - rad, x * S + rad, y * S + rad), fill=0)


def stroke(points):
    """Polyline with round caps and joins."""
    pts = [(x * S, y * S) for x, y in points]
    d.line(pts, fill=0, width=int(STROKE * S))
    for x, y in points:
        dot(x, y)


def bezier(p0, p1, p2, n=60):
    return [((1 - t) ** 2 * p0[0] + 2 * (1 - t) * t * p1[0] + t * t * p2[0],
             (1 - t) ** 2 * p0[1] + 2 * (1 - t) * t * p1[1] + t * t * p2[1])
            for t in (i / n for i in range(n + 1))]


def arc(cx, cy, rad, a0, a1, n=60):
    return [(cx + rad * math.cos(math.radians(a)), cy + rad * math.sin(math.radians(a)))
            for a in (a0 + (a1 - a0) * i / n for i in range(n + 1))]


spine_top, spine_bot = 330, 452
for side in (-1, 1):
    outer = 225 + side * 132
    # top edge of the page, sagging towards the spine
    stroke(bezier((225, spine_top), (225 + side * 60, 288), (outer, 302)))
    # outer edge
    stroke([(outer, 302), (outer, 430)])
    # bottom edge
    stroke(bezier((outer, 430), (225 + side * 62, 418), (225, spine_bot)))
    # cover peeking out under the pages
    stroke(bezier((225, spine_bot + 26), (225 + side * 70, 440), (outer + side * 22, 452)))
    stroke([(outer + side * 22, 452), (outer + side * 22, 330)])
stroke([(225, spine_top), (225, spine_bot)])

# Wi-Fi waves above the spine
cx, cy = 225, 262
dot(cx, cy, r * 1.25)
for rad in (42, 82, 122):
    stroke(arc(cx, cy, rad, 225, 315))

img = img.resize((W, H), Image.LANCZOS)
img.save(sys.argv[1])
