# Regenerates the Dear Chaya favicon set (static/img/favicon.*, icon-*.png,
# apple-touch-icon.png). Usage:
#   python3 tools/make_icon.py static/fonts/caveat-latin-15f9638095.woff2 static/img
# (It also writes a preview.png sheet; delete it afterwards.)
"""Dear Chaya favicon: a tiny spiral notebook page with a handwritten "C"."""
import sys
from PIL import Image, ImageDraw, ImageFont

FONT, OUT = sys.argv[1], sys.argv[2]
# Rule colour is pre-mixed onto the paper so it stays opaque on any tab colour.
PAPER, EDGE, RULE, MARGIN, INK, METAL = "#FAF7F2", "#E8E1D5", "#B9C6DD", "#C85A54", "#2B4C7E", "#4B5563"

def draw(size=512, rounded=True, simple=False):
    s = size / 512
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img, "RGBA")
    r = 96 * s if rounded else 0
    d.rounded_rectangle([0, 0, size - 1, size - 1], radius=r, fill=PAPER, outline=EDGE, width=round(8 * s))
    # Ruled lines and the red margin
    if not simple:
        for y in (196, 290, 384):
            d.rectangle([6 * s, y * s, size - 6 * s, (y + 7) * s], fill=RULE)
    d.rectangle([108 * s, (24 if simple else 64) * s, (118 if not simple else 132) * s, size - 6 * s], fill=MARGIN)
    # Spiral rings along the top edge
    for x in (() if simple else (150, 230, 310, 390)):
        d.rounded_rectangle([(x - 13) * s, -10 * s, (x + 13) * s, 70 * s], radius=13 * s, fill=METAL)
        d.ellipse([(x - 20) * s, 52 * s, (x + 20) * s, 92 * s], outline=METAL, width=round(7 * s))
    # The handwritten "C", resting on the bottom rule
    # (16px gets a bigger, heavier "C" and no rings or rules, which would blur.)
    font = ImageFont.truetype(FONT, round((520 if simple else 370) * s))
    d.text(((318 if simple else 305) * s, (440 if simple else 384) * s), "C", font=font, fill=INK, anchor="ms",
           stroke_width=round((22 if simple else 10) * s), stroke_fill=INK)
    return img

big = draw()
big.save(f"{OUT}/icon-512.png", optimize=True)
big.resize((192, 192), Image.LANCZOS).save(f"{OUT}/icon-192.png", optimize=True)
big.resize((64, 64), Image.LANCZOS).save(f"{OUT}/favicon.png", optimize=True)
# iOS applies its own rounded mask and shows transparency as black: full-bleed square.
draw(rounded=False).convert("RGB").resize((180, 180), Image.LANCZOS).save(f"{OUT}/apple-touch-icon.png", optimize=True)
small = draw(simple=True).resize((16, 16), Image.LANCZOS)
big.resize((48, 48), Image.LANCZOS).save(
    f"{OUT}/favicon.ico", sizes=[(16, 16), (32, 32), (48, 48)],
    append_images=[small, big.resize((32, 32), Image.LANCZOS)])

# Preview sheet: real tab sizes on light and dark tab bars.
sheet = Image.new("RGB", (560, 230), "#ffffff")
dark = Image.new("RGB", (560, 115), "#202124"); sheet.paste(dark, (0, 115))
x = 12
for px in (16, 32, 48, 96):
    ic = (draw(simple=True) if px == 16 else big).resize((px, px), Image.LANCZOS)
    for row, y0 in ((0, 0), (1, 115)):
        sheet.paste(ic, (x, y0 + (115 - px) // 2), ic)
    x += px + 30
sheet.paste(big.resize((192, 192), Image.LANCZOS), (340, 19), big.resize((192, 192), Image.LANCZOS))
sheet.resize((1120, 460), Image.NEAREST).save(f"{OUT}/preview.png")
