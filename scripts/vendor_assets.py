#!/usr/bin/env python3
"""Restore checksummed CN rules. The customized dashboard builds from frontend/."""
import hashlib
import json
from pathlib import Path
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
LOCK = ROOT / 'third_party' / 'assets.lock.json'


def main():
    lock = json.loads(LOCK.read_text())
    rules = ROOT / 'third_party' / 'rules'
    rules.mkdir(parents=True, exist_ok=True)
    for kind, name in [('geosite', 'cn-domain.mrs'), ('geoip', 'cn-ip.mrs')]:
        path = rules / name
        if path.exists():
            data = path.read_bytes()
        else:
            url = 'https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/' + kind + '/cn.mrs'
            data = urllib.request.urlopen(url, timeout=30).read()
        if hashlib.sha256(data).hexdigest() != lock['rules'][name]:
            raise RuntimeError('Rule checksum mismatch: ' + name)
        path.write_bytes(data)
    print('CN rules verified. Build the customized dashboard from frontend/.')


if __name__ == '__main__':
    main()
