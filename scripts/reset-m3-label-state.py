#!/usr/bin/env python3
import argparse
import sqlite3
from pathlib import Path


def main():
    ap = argparse.ArgumentParser(description="Reset M3.1 label state tables")
    ap.add_argument("--db", required=True, help="Path to .outtake.v2.sqlite")
    args = ap.parse_args()

    db = Path(args.db).expanduser()
    conn = sqlite3.connect(db)
    try:
        cur = conn.cursor()
        cur.execute("CREATE TABLE IF NOT EXISTS gmail_message_labels(messageId TEXT NOT NULL, label TEXT NOT NULL, updatedAtMs INTEGER NOT NULL, PRIMARY KEY(messageId, label))")
        cur.execute("DELETE FROM gmail_message_labels")
        conn.commit()
    finally:
        conn.close()

    print("reset complete: gmail_message_labels")


if __name__ == "__main__":
    main()
