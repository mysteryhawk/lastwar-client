#!/usr/bin/env python3
"""Extract selected Lua 5.3 modules from the historical official LWLF bundle.

python tools/extract_lua.py /path/LWScripts_826_u440.bz2 /tmp/lua --match EnumType --match Rank
Decompile fixed files with unluac. Output stays outside GitHub; no game login.
"""
import argparse
import bz2
import hashlib
import json
import struct
from pathlib import Path


def entries(data):
    if data[:4] != b'LWLF':
        raise ValueError('not LWLF')
    count = struct.unpack_from('<I',data,12)[0]
    offset = 16
    for index in range(count):
        size,shift = 0,0
        while True:
            byte = data[offset];offset += 1
            size |= (byte & 127) << shift;shift += 7
            if shift > 35:raise ValueError('invalid name length')
            if byte < 128:break
        name = data[offset:offset+size].decode();offset += size
        length = struct.unpack_from('<I',data,offset)[0];offset += 4
        chunk = data[offset:offset+length];offset += length
        if len(chunk) != length:raise ValueError('truncated chunk')
        yield index,name,chunk
    if offset != len(data):raise ValueError('unexpected archive suffix')


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('archive',type=Path);p.add_argument('output',type=Path)
    p.add_argument('--match',action='append',required=True)
    args=p.parse_args();raw=args.archive.read_bytes();data=bz2.decompress(raw)
    selected=[]
    for index,name,chunk in entries(data):
        if not any(term.lower() in name.lower() for term in args.match):continue
        relative=Path(name)
        if relative.is_absolute() or '..' in relative.parts:raise ValueError('unsafe archive name')
        if not chunk.startswith(b'\x1bLua\x53\x01'):raise ValueError('unexpected Lua header')
        target=args.output/relative;target.parent.mkdir(parents=True,exist_ok=True)
        target.write_bytes(chunk[:5]+b'\x00'+chunk[6:14]+b'\x04'+chunk[14:])
        selected.append(dict(index=index,name=name,sha256=hashlib.sha256(chunk).hexdigest()))
    args.output.mkdir(parents=True,exist_ok=True)
    (args.output/'manifest.json').write_text(json.dumps(dict(archive_sha256=hashlib.sha256(raw).hexdigest(),
        version=struct.unpack_from('<I',data,8)[0],selected=selected),indent=2))
    print('\n'.join(e['name'] for e in selected))

if __name__=='__main__':main()
