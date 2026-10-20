"""Generate Windowseat SVG, PNG, and ICO icons from one shape table."""

from pathlib import Path
from xml.sax.saxutils import escape

from PIL import Image, ImageDraw


HERE = Path(__file__).parent
SCALE = 8
SIZES = (16, 32, 64, 256)
COLORS = {
    "windowseat": "#328C9C",
    "state-ok": "#2480C2",
    "state-warning": "#D17A16",
    "state-stale": "#8D989F",
    "state-error": "#C74C72",
}

# The outer dark edge separates the pale frame from light tray backgrounds.
# The pale frame separates the dark aperture from dark tray backgrounds.
SHAPES = (
    ("rect", (2, 1, 28, 30, 12), "#192B39"),
    ("rect", (3, 2, 26, 28, 11), "#F5F7F4"),
    ("rect", (7, 5, 18, 22, 7), "#192B39"),
    (
        "path",
        (
            ("M", 7, 16),
            ("H", 25),
            ("V", 20),
            ("C", 25, 24, 22, 27, 18, 27),
            ("H", 14),
            ("C", 10, 27, 7, 24, 7, 20),
            ("Z",),
        ),
        "accent",
    ),
)


def svg_path(commands):
    return " ".join(" ".join(map(str, command)) for command in commands)


def svg(accent):
    lines = ['<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">']
    for kind, geometry, color in SHAPES:
        fill = accent if color == "accent" else color
        if kind == "rect":
            x, y, width, height, radius = geometry
            lines.append(
                f'  <rect x="{x}" y="{y}" width="{width}" height="{height}" '
                f'rx="{radius}" fill="{fill}"/>'
            )
        else:
            lines.append(f'  <path d="{escape(svg_path(geometry))}" fill="{fill}"/>')
    lines.append("</svg>")
    return "\n".join(lines) + "\n"


def path_points(commands):
    points = []
    current = (0, 0)
    for command in commands:
        name, *values = command
        if name == "M":
            current = tuple(values)
            points.append(current)
        elif name == "H":
            current = (values[0], current[1])
            points.append(current)
        elif name == "V":
            current = (current[0], values[0])
            points.append(current)
        elif name == "C":
            start = current
            control1 = (values[0], values[1])
            control2 = (values[2], values[3])
            end = (values[4], values[5])
            for step in range(1, 33):
                t = step / 32
                inverse = 1 - t
                points.append(
                    tuple(
                        inverse**3 * start[axis]
                        + 3 * inverse**2 * t * control1[axis]
                        + 3 * inverse * t**2 * control2[axis]
                        + t**3 * end[axis]
                        for axis in (0, 1)
                    )
                )
            current = end
    return points


def png(accent, size):
    factor = size * SCALE / 32
    canvas = Image.new("RGBA", (size * SCALE, size * SCALE), (0, 0, 0, 0))
    draw = ImageDraw.Draw(canvas)
    for kind, geometry, color in SHAPES:
        fill = accent if color == "accent" else color
        if kind == "rect":
            x, y, width, height, radius = geometry
            draw.rounded_rectangle(
                (x * factor, y * factor, (x + width) * factor, (y + height) * factor),
                radius=radius * factor,
                fill=fill,
            )
        else:
            draw.polygon([(x * factor, y * factor) for x, y in path_points(geometry)], fill=fill)
    return canvas.resize((size, size), Image.Resampling.LANCZOS)


def ico(accent, path):
    png(accent, 48).save(
        path,
        format="ICO",
        sizes=[(16, 16), (32, 32), (48, 48)],
        append_images=[png(accent, 16), png(accent, 32)],
    )


def main():
    for name, accent in COLORS.items():
        (HERE / f"{name}.svg").write_text(svg(accent), encoding="utf-8")
        for size in SIZES:
            png(accent, size).save(HERE / f"{name}-{size}.png")
        if name.startswith("state-"):
            ico(accent, HERE / f"{name}.ico")

    ico(COLORS["windowseat"], HERE / "favicon.ico")


if __name__ == "__main__":
    main()
