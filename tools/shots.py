"""Frames the README screenshots.

    cd work && CANDLE_README=<raw> go test -run TestReadmeShots
    python tools/shots.py <raw> docs/screenshots

TestReadmeShots draws the window at twice the size without what Windows
draws: there are no caption buttons. This draws them, rounds the corners,
adds the shadow, and writes WebP. hero.webp is the main chart, light on the
left and dark on the right. Needs Pillow.
"""

import os
import sys

from PIL import Image, ImageDraw, ImageFilter

S = 2  # pixels per DIP in the raw shots
BAR = 44  # the title bar's height, DIPs
LIGHT = {"glyph": (19, 23, 34), "edge": (0, 0, 0, 34)}
DARK = {"glyph": (209, 212, 220), "edge": (255, 255, 255, 30)}


def caption(im, look):
    """Draws the minimize, maximize and close glyphs in the title bar's right 138 DIPs."""
    d = ImageDraw.Draw(im)
    w = im.width
    y, g, lw = BAR // 2 * S, 5 * S, S
    for i in range(3):
        x = w - 138 * S + 23 * S + i * 46 * S
        if i == 0:
            d.line([(x - g, y), (x + g, y)], fill=look["glyph"], width=lw)
        elif i == 1:
            d.rectangle([x - g, y - g, x + g, y + g], outline=look["glyph"], width=lw)
        else:
            d.line([(x - g, y - g), (x + g, y + g)], fill=look["glyph"], width=lw)
            d.line([(x - g, y + g), (x + g, y - g)], fill=look["glyph"], width=lw)
    return im


def frame(im, look, width):
    """Rounds the window's corners, outlines it, drops a shadow and scales it to width."""
    r, pad = 8 * S, 72
    w, h = im.size
    mask = Image.new("L", (w, h), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, w - 1, h - 1], r, fill=255)
    win = im.convert("RGBA")
    win.putalpha(mask)
    ImageDraw.Draw(win).rounded_rectangle([0, 0, w - 1, h - 1], r, outline=look["edge"], width=2)
    out = Image.new("RGBA", (w + 2 * pad, h + 2 * pad), (0, 0, 0, 0))
    shadow = Image.new("L", out.size, 0)
    ImageDraw.Draw(shadow).rounded_rectangle([pad, pad + 20, pad + w, pad + h + 20], r, fill=70)
    shadow = shadow.filter(ImageFilter.GaussianBlur(28))
    out.putalpha(shadow)
    out.alpha_composite(win, (pad, pad))
    return out.resize((width, round(out.height * width / out.width)), Image.LANCZOS)


def save(im, path):
    im.save(path, "WEBP", quality=88, method=6)
    print(f"{path}  {im.width}x{im.height}  {os.path.getsize(path) // 1024} KB")


def main(raw, out):
    os.makedirs(out, exist_ok=True)
    load = lambda name: Image.open(os.path.join(raw, name + ".png")).convert("RGB")
    light, dark = load("main-light"), load("main-dark")
    w, h = light.size
    split = Image.new("L", (w, h), 0)
    ImageDraw.Draw(split).polygon([(int(w * 0.6), 0), (w, 0), (w, h), (int(w * 0.38), h)], fill=255)
    hero = Image.composite(dark, light, split)
    save(frame(caption(hero, DARK), LIGHT, 1800), os.path.join(out, "hero.webp"))
    for name in ["indicators-dark", "drawings-light", "settings-light", "indicators-list-dark"]:
        look = DARK if name.endswith("dark") else LIGHT
        save(frame(caption(load(name), look), look, 1300), os.path.join(out, name + ".webp"))


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
