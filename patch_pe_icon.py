import io
import os
import struct
import sys
from pathlib import Path

from PIL import Image


def align(v, a):
    return (v + a - 1) // a * a


def parse_ico_images(path):
    data = Path(path).read_bytes()
    reserved, typ, count = struct.unpack_from('<HHH', data, 0)
    if (reserved, typ) != (0, 1) or count <= 0:
        raise ValueError('not a valid ICO')
    out = []
    for i in range(count):
        off = 6 + i * 16
        w8, h8, _cc, _rsv, _planes, _bpp, size, data_off = struct.unpack_from('<BBBBHHII', data, off)
        w = 256 if w8 == 0 else w8
        h = 256 if h8 == 0 else h8
        payload = data[data_off:data_off + size]
        if payload.startswith(b'\x89PNG\r\n\x1a\n'):
            img = Image.open(io.BytesIO(payload)).convert('RGBA')
        else:
            # Reconstruct a one-frame ICO so Pillow can decode DIB-backed source frames too.
            header = struct.pack('<HHH', 0, 1, 1)
            entry = struct.pack('<BBBBHHII', w8, h8, 0, 0, 1, 32, len(payload), 22)
            img = Image.open(io.BytesIO(header + entry + payload)).convert('RGBA')
        if img.size != (w, h):
            img = img.resize((w, h), Image.Resampling.LANCZOS)
        out.append((w, h, img))
    return out


def rgba_to_icon_dib(img):
    img = img.convert('RGBA')
    w, h = img.size
    rgba = img.tobytes()

    # ICO/RT_ICON DIB: BITMAPINFOHEADER, BGRA pixels bottom-up, then 1-bpp AND mask.
    xor = bytearray(w * h * 4)
    for y in range(h):
        src_y = h - 1 - y
        for x in range(w):
            si = (src_y * w + x) * 4
            di = (y * w + x) * 4
            r, g, b, a = rgba[si:si + 4]
            xor[di:di + 4] = bytes((b, g, r, a))

    mask_stride = ((w + 31) // 32) * 4
    and_mask = bytes(mask_stride * h)  # alpha channel is authoritative; mask stays transparent-safe.
    header = struct.pack(
        '<IiiHHIIiiII',
        40, w, h * 2, 1, 32, 0, len(xor), 0, 0, 0, 0
    )
    return header + xor + and_mask


def make_resource_entries(ico_path):
    entries = []
    for w, h, img in parse_ico_images(ico_path):
        dib = rgba_to_icon_dib(img)
        entries.append((w, h, 1, 32, dib))
    return entries


def write_normalized_ico(src, dst):
    entries = make_resource_entries(src)
    header = bytearray(struct.pack('<HHH', 0, 1, len(entries)))
    offset = 6 + 16 * len(entries)
    payloads = []
    for w, h, planes, bpp, dib in entries:
        w8 = 0 if w >= 256 else w
        h8 = 0 if h >= 256 else h
        header += struct.pack('<BBBBHHII', w8, h8, 0, 0, planes, bpp, len(dib), offset)
        payloads.append(dib)
        offset += len(dib)
    Path(dst).write_bytes(bytes(header) + b''.join(payloads))


def build_rsrc(entries, base_rva):
    n = len(entries)
    root = 0
    root_size = 16 + 2 * 8
    icon_type = root_size
    icon_type_size = 16 + n * 8
    group_type = icon_type + icon_type_size
    group_type_size = 16 + 8
    lang_start = group_type + group_type_size
    icon_lang = [lang_start + i * 24 for i in range(n)]
    group_lang = lang_start + n * 24
    data_entries = group_lang + 24
    data_entry_offsets = [data_entries + i * 16 for i in range(n + 1)]
    data_start = align(data_entries + (n + 1) * 16, 4)

    data_offsets = []
    cur = data_start
    for _w, _h, _planes, _bpp, payload in entries:
        cur = align(cur, 4)
        data_offsets.append(cur)
        cur += len(payload)

    grp = bytearray(struct.pack('<HHH', 0, 1, n))
    for idx, (w, h, planes, bpp, payload) in enumerate(entries, 1):
        grp += struct.pack(
            '<BBBBHHIH',
            0 if w >= 256 else w,
            0 if h >= 256 else h,
            0, 0, planes, bpp, len(payload), idx
        )

    cur = align(cur, 4)
    group_data_off = cur
    cur += len(grp)
    blob = bytearray(align(cur, 4))

    def dirhdr(off, id_count):
        struct.pack_into('<IIHHHH', blob, off, 0, 0, 0, 0, 0, id_count)

    dirhdr(root, 2)
    struct.pack_into('<II', blob, root + 16, 3, 0x80000000 | icon_type)   # RT_ICON
    struct.pack_into('<II', blob, root + 24, 14, 0x80000000 | group_type) # RT_GROUP_ICON

    dirhdr(icon_type, n)
    for i in range(n):
        struct.pack_into('<II', blob, icon_type + 16 + i * 8, i + 1, 0x80000000 | icon_lang[i])
    dirhdr(group_type, 1)
    struct.pack_into('<II', blob, group_type + 16, 1, 0x80000000 | group_lang)

    for i, lang_off in enumerate(icon_lang):
        dirhdr(lang_off, 1)
        struct.pack_into('<II', blob, lang_off + 16, 1033, data_entry_offsets[i])
    dirhdr(group_lang, 1)
    struct.pack_into('<II', blob, group_lang + 16, 1033, data_entry_offsets[-1])

    for i, entry in enumerate(entries):
        payload = entry[4]
        struct.pack_into('<IIII', blob, data_entry_offsets[i], base_rva + data_offsets[i], len(payload), 0, 0)
        blob[data_offsets[i]:data_offsets[i] + len(payload)] = payload

    struct.pack_into('<IIII', blob, data_entry_offsets[-1], base_rva + group_data_off, len(grp), 0, 0)
    blob[group_data_off:group_data_off + len(grp)] = grp
    return bytes(blob)


def patch(exe, ico, out):
    data = bytearray(Path(exe).read_bytes())
    pe = struct.unpack_from('<I', data, 0x3C)[0]
    if data[pe:pe + 4] != b'PE\0\0':
        raise ValueError('not PE')
    ns = struct.unpack_from('<H', data, pe + 6)[0]
    size_opt = struct.unpack_from('<H', data, pe + 20)[0]
    opt = pe + 24
    magic = struct.unpack_from('<H', data, opt)[0]
    if magic != 0x20B:
        raise ValueError('expected PE32+')
    section_align = struct.unpack_from('<I', data, opt + 32)[0]
    file_align = struct.unpack_from('<I', data, opt + 36)[0]
    sh = opt + size_opt

    sections = []
    max_end_va = 0
    max_end_raw = 0
    for i in range(ns):
        o = sh + i * 40
        vs, va, rs, rp = struct.unpack_from('<IIII', data, o + 8)
        sections.append((o, vs, va, rs, rp))
        max_end_va = max(max_end_va, va + max(vs, rs))
        max_end_raw = max(max_end_raw, rp + rs)

    first_raw = min(rp for _o, _vs, _va, _rs, rp in sections if rp)
    if sh + (ns + 1) * 40 > first_raw:
        raise ValueError('no section-header room')

    new_va = align(max_end_va, section_align)
    entries = make_resource_entries(ico)
    blob = build_rsrc(entries, new_va)
    new_raw = align(max(len(data), max_end_raw), file_align)
    raw_size = align(len(blob), file_align)

    if len(data) < new_raw:
        data.extend(b'\0' * (new_raw - len(data)))
    data.extend(blob)
    data.extend(b'\0' * (raw_size - len(blob)))

    o = sh + ns * 40
    hdr = bytearray(40)
    hdr[:8] = b'.rsrc\0\0\0'
    struct.pack_into('<IIIIIIHHI', hdr, 8, len(blob), new_va, raw_size, new_raw, 0, 0, 0, 0, 0x40000040)
    data[o:o + 40] = hdr
    struct.pack_into('<H', data, pe + 6, ns + 1)
    struct.pack_into('<I', data, opt + 56, align(new_va + len(blob), section_align))
    old_init = struct.unpack_from('<I', data, opt + 8)[0]
    struct.pack_into('<I', data, opt + 8, old_init + raw_size)

    # PE32+ data directories start at OptionalHeader+112; entry 2 is resources.
    resource_dd = opt + 112 + 2 * 8
    struct.pack_into('<II', data, resource_dd, new_va, len(blob))
    Path(out).write_bytes(data)


if __name__ == '__main__':
    if len(sys.argv) == 4 and sys.argv[1] == '--normalize':
        write_normalized_ico(sys.argv[2], sys.argv[3])
    elif len(sys.argv) == 4:
        patch(sys.argv[1], sys.argv[2], sys.argv[3])
    else:
        print('usage: patch_pe_icon.py EXE ICO OUT\n   or: patch_pe_icon.py --normalize ICO OUT', file=sys.stderr)
        raise SystemExit(2)
