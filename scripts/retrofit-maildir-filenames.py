#!/usr/bin/env python3
import argparse
from pathlib import Path


def parse_message_id(name: str) -> str | None:
    base = name.split(":", 1)[0]
    if base.endswith(".mail"):
        return base[:-5]
    if base.startswith("history."):
        return base[len("history.") :]
    if "." in base:
        left, right = base.split(".", 1)
        if left.isdigit() and right:
            return right
    return None


def iter_files(maildir: Path):
    for sub in ("new", "cur"):
        p = maildir / sub
        if not p.exists():
            continue
        for fp in p.iterdir():
            if fp.is_file():
                yield sub, fp


def main():
    ap = argparse.ArgumentParser(description="Rename legacy outtake filenames to <gmailMessageId>.mail")
    ap.add_argument("maildir")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    maildir = Path(args.maildir).expanduser()
    scanned = renamed = skipped = collisions = unmapped = 0

    for sub, fp in iter_files(maildir):
        scanned += 1
        old_name = fp.name
        msg_id = parse_message_id(old_name)
        if not msg_id:
            unmapped += 1
            continue

        suffix = ""
        if sub == "cur" and ":" in old_name:
            suffix = ":" + old_name.split(":", 1)[1]
        target = f"{msg_id}.mail{suffix}"
        if target == old_name:
            skipped += 1
            continue

        dst = fp.with_name(target)
        if dst.exists():
            collisions += 1
            continue

        renamed += 1
        if not args.dry_run:
            fp.rename(dst)

    print(
        f"scanned={scanned} renamed={renamed} skipped={skipped} collisions={collisions} unmapped={unmapped} dry_run={args.dry_run}"
    )


if __name__ == "__main__":
    main()
