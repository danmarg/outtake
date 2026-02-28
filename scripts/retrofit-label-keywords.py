#!/usr/bin/env python3
import argparse
import sqlite3
from pathlib import Path


def load_labels(db_path: Path) -> dict[str, str]:
    conn = sqlite3.connect(db_path)
    try:
        cur = conn.cursor()
        cur.execute("SELECT id, name FROM gmail_labels")
        return {row[0]: row[1] for row in cur.fetchall()}
    finally:
        conn.close()


def split_headers_body(raw: bytes):
    idx = raw.find(b"\r\n\r\n")
    if idx >= 0:
        return raw[:idx], raw[idx + 4 :], b"\r\n"
    idx = raw.find(b"\n\n")
    if idx >= 0:
        return raw[:idx], raw[idx + 2 :], b"\n"
    return raw, b"", b"\n"


def rewrite_keywords(raw: bytes, label_map: dict[str, str]):
    headers, body, nl = split_headers_body(raw)
    lines = headers.splitlines(keepends=True)
    changed = False
    out = []

    for line in lines:
        lower = line.lower()
        if lower.startswith(b"x-keywords:"):
            parts = line.split(b":", 1)
            if len(parts) == 2:
                value = parts[1].strip().decode("utf-8", errors="replace")
                mapped = label_map.get(value)
                if mapped and mapped != value:
                    out.append(b"X-Keywords: " + mapped.encode("utf-8") + nl)
                    changed = True
                    continue
        out.append(line)

    if not changed:
        return raw, False

    sep = b"\r\n\r\n" if nl == b"\r\n" else b"\n\n"
    return b"".join(out) + sep + body, True


def iter_mail_files(maildir: Path):
    for sub in ("new", "cur"):
        p = maildir / sub
        if not p.exists():
            continue
        for item in p.iterdir():
            if item.is_file():
                yield item


def main():
    ap = argparse.ArgumentParser(description="Retrofit Maildir X-Keywords headers from Gmail label IDs to label names")
    ap.add_argument("maildir", help="Maildir path (contains new/cur)")
    ap.add_argument("--db", help="Path to .outtake.v2.sqlite (default: <maildir>/.outtake.v2.sqlite)")
    ap.add_argument("--dry-run", action="store_true", help="Report only, do not modify files")
    args = ap.parse_args()

    maildir = Path(args.maildir).expanduser()
    db = Path(args.db).expanduser() if args.db else (maildir / ".outtake.v2.sqlite")

    label_map = load_labels(db)
    scanned = 0
    changed = 0

    for fp in iter_mail_files(maildir):
        scanned += 1
        raw = fp.read_bytes()
        new_raw, did_change = rewrite_keywords(raw, label_map)
        if did_change:
            changed += 1
            if not args.dry_run:
                tmp = fp.with_suffix(fp.suffix + ".tmp")
                tmp.write_bytes(new_raw)
                tmp.replace(fp)

    print(f"scanned={scanned} changed={changed} dry_run={args.dry_run}")


if __name__ == "__main__":
    main()
