"""Generate Windowseat PNG and ICO icons from source/windowseat-master.png.

The master was drawn by Codex image generation. Each pixel is snapped to the
nearest of three flat colours, so the state icons can swap the teal gauge arc
for a state colour without touching the frame or needle.
"""

from pathlib import Path

from PIL import Image


HERE = Path(__file__).parent
MASTER = HERE / "source" / "windowseat-master.png"
SIZES = (16, 32, 64, 256)

NAVY = (15, 40, 64)
PAPER = (249, 247, 243)
TEAL = (23, 186, 172)

ACCENTS = {
    "windowseat": TEAL,
    "state-ok": (36, 128, 194),
    "state-warning": (209, 122, 22),
    "state-stale": (141, 152, 159),
    "state-error": (199, 76, 114),
}


def classify(pixel):
    """Teal is anything clearly green-blue; the rest is whichever of navy or paper is closer.

    A plain nearest-colour match would turn navy-to-paper edge pixels teal.
    """
    red, green, _ = pixel
    if green - red > 60:
        return TEAL
    return min((NAVY, PAPER), key=lambda c: sum((a - b) ** 2 for a, b in zip(pixel, c)))


def flat_master():
    """Return the master's alpha and a mask of its teal pixels."""
    master = Image.open(MASTER).convert("RGBA")
    alpha = master.getchannel("A").point(lambda a: 255 if a >= 250 else a)
    classes = [classify(p[:3]) for p in master.get_flattened_data()]
    base = Image.new("RGB", master.size)
    base.putdata([c if c != TEAL else PAPER for c in classes])
    accent = Image.new("L", master.size)
    accent.putdata([255 if c == TEAL else 0 for c in classes])
    return base, accent, alpha


def icon(base, accent_mask, alpha, accent, size):
    full = Image.composite(Image.new("RGB", base.size, accent), base, accent_mask)
    full.putalpha(alpha)
    return full.resize((size, size), Image.Resampling.LANCZOS)


def ico(images, path):
    images[48].save(path, format="ICO", sizes=[(16, 16), (32, 32), (48, 48)],
                    append_images=[images[16], images[32]])


def main():
    base, accent_mask, alpha = flat_master()
    for name, accent in ACCENTS.items():
        images = {size: icon(base, accent_mask, alpha, accent, size) for size in (*SIZES, 48)}
        for size in SIZES:
            images[size].save(HERE / f"{name}-{size}.png")
        if name.startswith("state-"):
            ico(images, HERE / f"{name}.ico")
        else:
            ico(images, HERE / "favicon.ico")


if __name__ == "__main__":
    main()
