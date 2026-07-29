#!/usr/bin/env python3
"""Build a one-shot migration document without printing secret values."""

import json
import os
import pathlib
import sys


ROOT = pathlib.Path(__file__).resolve().parents[1]
STATE = ROOT / "state" / "peers"
SECRETS = ROOT / "secrets" / "peers"
PLATFORMS = {"windows", "macos", "ios", "android"}


def read_meta(path: pathlib.Path) -> dict[str, str]:
    result: dict[str, str] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        result[key] = value
    return result


def main() -> None:
    devices = []
    for path in sorted(STATE.glob("*.meta")):
        meta = read_meta(path)
        name = meta.get("name", "")
        required = {"name", "ipv4", "ipv6", "public_key", "created_at", "status"}
        if not required.issubset(meta) or meta["status"] != "active":
            raise SystemExit(f"invalid active peer metadata: {path}")
        if name not in PLATFORMS or path.stem != name:
            raise SystemExit(f"unsupported legacy peer: {name}")
        psk_path = SECRETS / name / "psk"
        if not psk_path.is_file():
            raise SystemExit(f"missing PSK for {name}")
        mode = os.stat(psk_path).st_mode & 0o777
        if mode & 0o077:
            raise SystemExit(f"unsafe PSK permissions for {name}")
        devices.append(
            {
                "name": name,
                "platform": name,
                "ipv4": meta["ipv4"],
                "ipv6": meta["ipv6"],
                "public_key": meta["public_key"],
                "preshared_key": psk_path.read_text(encoding="ascii").strip(),
                "created_at": meta["created_at"],
            }
        )
    if not devices:
        raise SystemExit("no active peers found")
    json.dump({"devices": devices}, sys.stdout, separators=(",", ":"))
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
