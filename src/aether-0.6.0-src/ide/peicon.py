#!/usr/bin/env python3
"""Replace the application icon inside a Windows PE (.exe) in place, without rc/rcedit/wine.
Every RT_ICON image referenced by every RT_GROUP_ICON is overwritten with a PNG-encoded image of the
same size (PNG icons are supported since Windows Vista). PNG data is much smaller than the original
BMP data, so it always fits into the existing resource slot; the slot is zero-padded and its size updated.
usage: peicon.py app.exe icon-1024.png"""
import io, struct, sys
import pefile
from PIL import Image

RT_ICON, RT_GROUP_ICON = 3, 14


def main(exe, png):
    pe = pefile.PE(exe, fast_load=True)
    pe.parse_data_directories(directories=[pefile.DIRECTORY_ENTRY["IMAGE_DIRECTORY_ENTRY_RESOURCE"]])
    big = Image.open(png).convert("RGBA")
    icons, groups = {}, []
    for t in pe.DIRECTORY_ENTRY_RESOURCE.entries:
        for e in t.directory.entries:
            for l in e.directory.entries:
                if t.id == RT_ICON:
                    icons[e.id] = l.data.struct
                elif t.id == RT_GROUP_ICON:
                    groups.append(l.data.struct)
    if not groups:
        sys.exit("no RT_GROUP_ICON in " + exe)
    data = bytearray(pe.__data__)
    done = 0
    for g in groups:
        off = pe.get_offset_from_rva(g.OffsetToData)
        _, _, n = struct.unpack_from("<HHH", data, off)
        for i in range(n):
            eo = off + 6 + 14 * i
            w, h, colors, res, planes, bpp, size, nid = struct.unpack_from("<BBBBHHIH", data, eo)
            px = w or 256
            buf = io.BytesIO()
            big.resize((px, px), Image.LANCZOS).save(buf, "PNG", optimize=True)
            img = buf.getvalue()
            slot = icons[nid]
            if len(img) > slot.Size:
                print(f"skip {px}px: png {len(img)} > slot {slot.Size}")
                continue
            so = pe.get_offset_from_rva(slot.OffsetToData)
            data[so:so + slot.Size] = img + b"\0" * (slot.Size - len(img))
            # resource data entry size (struct file offset) and group entry
            struct.pack_into("<I", data, slot.get_file_offset() + 4, len(img))
            struct.pack_into("<BBBBHHIH", data, eo, w, h, 0, 0, 1, 32, len(img), nid)
            done += 1
    pe.close()
    pe2 = pefile.PE(data=bytes(data), fast_load=True)
    pe2.OPTIONAL_HEADER.CheckSum = pe2.generate_checksum()
    out = pe2.write()
    pe2.close()
    open(exe, "wb").write(bytes(out))
    print(f"replaced {done} icon images in {exe}")
    return done


if __name__ == "__main__":
    sys.exit(0 if main(sys.argv[1], sys.argv[2]) else 1)
