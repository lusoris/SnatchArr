# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
"""Write a trackerless single-file or folder .torrent (BitTorrent v1).

    python3 mktorrent.py SOURCE OUT.torrent

The harness seeds SOURCE from qBittorrent, so the *arr sees a finished download without a
peer. Prints the info hash.
"""

import hashlib
import pathlib
import sys

PIECE = 1 << 18


_END = object()


def bencode(value):
    # An explicit stack instead of recursion: _END closes the list or dict opened before it.
    out, stack = [], [value]
    while stack:
        v = stack.pop()
        if isinstance(v, str):
            v = v.encode()
        if v is _END:
            out.append(b"e")
        elif isinstance(v, int):
            out.append(b"i%de" % v)
        elif isinstance(v, bytes):
            out.append(b"%d:%s" % (len(v), v))
        elif isinstance(v, list):
            out.append(b"l")
            stack.append(_END)
            stack.extend(reversed(v))
        elif isinstance(v, dict):
            out.append(b"d")
            stack.append(_END)
            for k, item in sorted(
                ((k.encode(), item) for k, item in v.items()), reverse=True
            ):
                stack.extend((item, k))
        else:
            raise TypeError(f"cannot bencode {type(v).__name__}")
    return b"".join(out)


def pieces(paths):
    out, buf = [], b""
    for path in paths:
        with path.open("rb") as f:
            while chunk := f.read(PIECE):
                buf += chunk
                while len(buf) >= PIECE:
                    out.append(hashlib.sha1(buf[:PIECE]).digest())
                    buf = buf[PIECE:]
    if buf:
        out.append(hashlib.sha1(buf).digest())
    return b"".join(out)


def info_dict(source):
    info = {"name": source.name, "piece length": PIECE}
    if source.is_file():
        info["length"] = source.stat().st_size
        info["pieces"] = pieces([source])
        return info
    files = sorted(p for p in source.rglob("*") if p.is_file())
    info["files"] = [
        {"length": p.stat().st_size, "path": list(p.relative_to(source).parts)}
        for p in files
    ]
    info["pieces"] = pieces(files)
    return info


def main(argv):
    if len(argv) != 3:
        sys.exit("usage: mktorrent.py SOURCE OUT.torrent")
    info = info_dict(pathlib.Path(argv[1]))
    pathlib.Path(argv[2]).write_bytes(bencode({"info": info}))
    print(hashlib.sha1(bencode(info)).hexdigest())


if __name__ == "__main__":
    main(sys.argv)
