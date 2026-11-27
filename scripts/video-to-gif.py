#!/usr/bin/env python3
"""Convert a complete video to an optimized GIF using FFmpeg."""

import argparse
from pathlib import Path
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    parser.add_argument("destination", type=Path)
    parser.add_argument("--width", type=int, default=800)
    parser.add_argument("--fps", type=int, default=5)
    args = parser.parse_args()
    if not args.source.is_file():
        parser.error("Source video does not exist")
    if args.width < 1 or args.fps < 1:
        parser.error("Width and fps must be positive")
    args.destination.parent.mkdir(parents=True, exist_ok=True)
    filters = f"fps={args.fps},scale={args.width}:-1:flags=lanczos"
    with tempfile.TemporaryDirectory(dir=args.destination.parent) as temp:
        palette = Path(temp) / "palette.png"
        output = Path(temp) / "preview.gif"
        common = ["ffmpeg", "-hide_banner", "-loglevel", "warning", "-y", "-i", str(args.source)]
        print("Generating color palette...", flush=True)
        subprocess.run(common + ["-vf", filters + ",palettegen=stats_mode=diff", "-frames:v", "1", "-update", "1", str(palette)], check=True)
        print("Encoding full video as GIF...", flush=True)
        subprocess.run(common + ["-i", str(palette), "-filter_complex", f"[0:v]{filters}[scaled];[scaled][1:v]paletteuse=dither=bayer:bayer_scale=3:diff_mode=rectangle", "-an", "-loop", "0", str(output)], check=True)
        output.replace(args.destination)
    print(f"Saved {args.destination} ({args.destination.stat().st_size:,} bytes)", flush=True)


if __name__ == "__main__":
    main()
