#!/usr/bin/env python3
"""生成应用图标与托盘图标（纯标准库，无需 PIL）。
输出 64x64 RGBA PNG：
  - assets/icon.png       应用图标（蓝底 + 白色中心圆点，示意 Wi‑Fi 终端）
  - assets/icon_main.png  主网状态托盘（绿色）
  - assets/icon_backup.png 备用状态托盘（红色）
"""
import os
import zlib
import struct

SIZE = 64


def write_png(path, rgba):
    def chunk(typ, data):
        body = typ + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body) & 0xFFFFFFFF)

    sig = b"\x89PNG\r\n\x1a\n"
    ihdr = struct.pack(">IIBBBBB", SIZE, SIZE, 8, 6, 0, 0, 0)  # 8bit, RGBA
    raw = bytearray()
    for y in range(SIZE):
        raw.append(0)  # filter type 0
        raw.extend(rgba[y * SIZE * 4:(y + 1) * SIZE * 4])
    comp = zlib.compress(bytes(raw), 9)
    with open(path, "wb") as f:
        f.write(sig)
        f.write(chunk(b"IHDR", ihdr))
        f.write(chunk(b"IDAT", comp))
        f.write(chunk(b"IEND", b""))


def make_disc(path, color, center_dot=False):
    r, g, b = color
    buf = bytearray(SIZE * SIZE * 4)
    cx = cy = SIZE / 2.0
    radius = SIZE / 2.0 - 2
    dot_r = radius * 0.28
    for y in range(SIZE):
        for x in range(SIZE):
            dx = x - cx + 0.5
            dy = y - cy + 0.5
            d = (dx * dx + dy * dy) ** 0.5
            idx = (y * SIZE + x) * 4
            if d <= radius:
                if center_dot and d <= dot_r:
                    buf[idx] = 255
                    buf[idx + 1] = 255
                    buf[idx + 2] = 255
                else:
                    buf[idx] = r
                    buf[idx + 1] = g
                    buf[idx + 2] = b
                buf[idx + 3] = 255
            else:
                buf[idx + 3] = 0
    write_png(path, bytes(buf))


here = os.path.dirname(os.path.abspath(__file__))
assets = os.path.join(here, "assets")
os.makedirs(assets, exist_ok=True)

make_disc(os.path.join(assets, "icon.png"), (37, 99, 235), center_dot=True)       # blue, app
make_disc(os.path.join(assets, "icon_main.png"), (46, 160, 67))                   # green, main
make_disc(os.path.join(assets, "icon_backup.png"), (200, 50, 50))                 # red, backup
print("icons generated:", os.listdir(assets))
