"""Generate a command-line-only banner in the OpenWrt CLI README style (Pillow)."""
import argparse
from pathlib import Path
from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--font', type=Path, required=True)
parser.add_argument('--out', type=Path, default=ROOT / 'assets/demos/cli.gif')
args = parser.parse_args()
font = ImageFont.truetype(str(args.font), 20)
commands = [
    'govmc profiles list',
    'govmc --profile lab vm list',
    'govmc vm clone template demo --dry-run',
    'govmc tasks list',
    'govmc mcp serve',
    'govmc serve',
    'govmc tui',
]
frames, durations = [], []

def frame(text, cursor=True):
    image = Image.new('RGB', (800, 60), '#000000')
    draw = ImageDraw.Draw(image)
    width = draw.textlength('$ ' + max(commands, key=len), font=font)
    x, y = round((800 - width - 10) / 2), 15
    draw.text((x, y), '$ ', font=font, fill='#b2bdc5')
    x += draw.textlength('$ ', font=font)
    prefix = text[:5]
    draw.text((x, y), prefix, font=font, fill='#4dd5ff')
    x += draw.textlength(prefix, font=font)
    draw.text((x, y), text[5:], font=font, fill='#f5f5f5')
    x += draw.textlength(text[5:], font=font)
    if cursor:
        draw.rectangle((round(x)+1,19,round(x)+8,38), fill='#4dd5ff')
    return image.quantize(colors=64)

for command in commands:
    for end in range(1, len(command)+1):
        frames.append(frame(command[:end])); durations.append(65)
    for cursor in (True, False, True, False):
        frames.append(frame(command, cursor)); durations.append(260)
args.out.parent.mkdir(parents=True, exist_ok=True)
frames[0].save(args.out, save_all=True, append_images=frames[1:], duration=durations, loop=0, optimize=True)
print(args.out)
