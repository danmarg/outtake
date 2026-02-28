# Architecture V2 Plan (Minimal)

## Goal
Use SQLite only for resumable sync cursors and auth token state.
Message storage lives in Maildir.

## Storage model

- **Maildir**: canonical message storage (raw messages/files).
- **SQLite**: control-plane state only.

No persistent per-message index table in SQLite.

## SQLite schema

```sql
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS sync_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updatedAtMs INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS oauth_tokens (
  account TEXT PRIMARY KEY,
  tokenType TEXT,
  accessToken TEXT,
  refreshToken TEXT,
  expiryUnixMs INTEGER,
  scope TEXT,
  rawJson TEXT NOT NULL,
  updatedAtMs INTEGER NOT NULL
);
```

## Cursors kept in `sync_state`

- `sync.list.next_page_token`
- `sync.list.done`
- `sync.materialize.cursor.response_id`
- `sync.materialize.cursor.message_id`
- `sync.history.cursor.committed` (future)
- `sync.history.cursor.progress` (future)
- `sync.history.page_token` (future)

## Sync behavior

1. List pages from Gmail using `nextPageToken` cursor in SQLite.
2. Write messages to Maildir.
3. Advance cursor only after successful durable progress.
4. On crash, resume from stored cursor.

## Notes

- Keep schema intentionally small to minimize SQLite cost.
- Avoid duplicating message corpus in DB.
- Maildir remains the source of truth for synced messages.
