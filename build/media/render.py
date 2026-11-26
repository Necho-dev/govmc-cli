"""Render captured tcell simulator cells into README PNGs for README screenshots.

Requires Pillow. Capture first with GOVMC_TUI_CAPTURE_DIR and the TUI simulator test.
No live ESXi credentials or machine data are used.
"""
import argparse
import json
from pathlib import Path
import unicodedata
from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parents[2]


def first_font(paths):
    for path in paths:
        if Path(path).is_file():
            return path
    raise SystemExit('Font not found; provide --font / --cjk-font')


def render(path, font, cjk):
    data = json.loads(path.read_text())
    cw, ch, pad, top = 10, 21, 18, 40
    image = Image.new('RGB', (data['W'] * cw + 2 * pad, data['H'] * ch + top + pad), '#1e1e1e')
    draw = ImageDraw.Draw(image)
    draw.text((pad, 10), 'GoVMC  /  simulator demonstration', font=font, fill='#9ba7b0')
    for i, cell in enumerate(data['Cells']):
        x, y = pad + i % data['W'] * cw, top + i // data['W'] * ch
        bg = cell['BG'] if cell['BG'] >= 0 else 0x1e1e1e
        fg = cell['FG'] if cell['FG'] >= 0 else 0xdddddd
        draw.rectangle((x, y, x + cw - 1, y + ch - 1), fill=((bg >> 16) & 255, (bg >> 8) & 255, bg & 255))
    for i, cell in enumerate(data['Cells']):
        text = cell['Text']
        if not text or text.isspace():
            continue
        x, y = pad + i % data['W'] * cw, top + i // data['W'] * ch
        fg = cell['FG'] if cell['FG'] >= 0 else 0xdddddd
        color = ((fg >> 16) & 255, (fg >> 8) & 255, fg & 255)
        chosen = cjk if unicodedata.east_asian_width(text[0]) in ('W', 'F') else font
        draw.text((x, y + 2), text, font=chosen, fill=color)
    return image


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--captures', type=Path, required=True)
    parser.add_argument('--font')
    parser.add_argument('--cjk-font')
    parser.add_argument('--out', type=Path, default=ROOT / 'assets')
    args = parser.parse_args()
    font_path = args.font or first_font([str(Path.home() / 'Library/Fonts/JetBrainsMonoNerdFont-Regular.ttf'), '/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf'])
    cjk_path = args.cjk_font or first_font(['/System/Library/Fonts/STHeiti Light.ttc', '/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc'])
    font = ImageFont.truetype(font_path, 16)
    cjk = ImageFont.truetype(cjk_path, 17)
    destination = args.out / 'screenshots/tui'
    destination.mkdir(parents=True, exist_ok=True)
    for name in ['overview', 'vms', 'details', 'datastores', 'environments']:
        image = render(args.captures / (name + '.json'), font, cjk)
        image.save(destination / (('machines' if name == 'vms' else name) + '.png'), optimize=True)

if __name__ == '__main__':
    main()
