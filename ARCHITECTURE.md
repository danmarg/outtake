# Architecture

This document explains how `outtake` syncs Gmail into a Maildir, with special focus on why an initial sync can take hours and why failures are expensive to reproduce.

## High-level flow

`main.go` constructs a `Gmail` synchronizer and calls:

- `Sync(full=false|true, progress)`

`Sync()` chooses one of two modes:

1. **Incremental sync** (`incremental(historyIndex)`), when a cached history checkpoint exists and `--full` is not set.
2. **Full sync** (`full()`), when there is no checkpoint, checkpoint is invalid/expired, or `--full` is set.

State is persisted in a BoltDB file (`.outtake`) in the target maildir.

---

## Components

- **CLI / orchestrator** (`main.go`)
  - Parses flags (`--directory`, `--label`, `--full`, `--buffer`, `--parallel`, etc.)
  - Creates progress printer

- **Sync engine** (`lib/gmail/gmail.go`)
  - Decides full vs incremental mode
  - Runs worker pools
  - Applies ADD / DELETE / WRITE_LABELS operations

- **Gmail API adapter** (`lib/gmail/service.go`)
  - Wraps Gmail REST calls
  - Applies request rate limiting and retry/backoff

- **Local metadata cache** (`lib/gmail/cache.go`, `lib/cache.go`)
  - Stores:
    - `history_index`
    - message-id -> maildir key
    - message-id -> labels
    - oauth token

- **Maildir writer** (`lib/maildir/maildir.go`)
  - Writes messages into `tmp/` then moves to `new/`
  - Reads and deletes existing messages by key

---

## Full sync (initial sync) in detail

Initial sync is `full()`. This is the expensive path for large inboxes.

### 1) List all messages

`GetMessages()` paginates `Users.Messages.List("me")` (query includes `-in:chats`).

For each page:
- enqueue message IDs onto `newMsgs` channel
- add IDs to in-memory `seen` map

### 2) Process message IDs in parallel

`ConcurrentDownloads` worker goroutines (default: 8) consume `newMsgs`.

Each message runs through `handleNewMsg(id)`:

- If message ID is not in local cache:
  - download full raw MIME (`GetRawMessage`)
  - parse MIME
  - mark operation as `ADD`
- Always fetch metadata (`GetMetadata`) for labels + history ID
- If cached message exists but labels changed: operation `WRITE_LABELS`
- Otherwise: `NONE`

### 3) Apply operations

Main loop consumes ops and writes to local storage:

- `ADD`:
  - deliver message to maildir
  - persist msg->key and msg->labels in cache
- `WRITE_LABELS`:
  - read existing maildir message
  - rewrite `X-Keywords` header
  - redeliver message, delete old file
  - update cache
- `DELETE`:
  - remove message file and cache entries

### 4) Reconcile deletions after processing

After workers complete, all cached IDs are scanned:
- if an ID is missing from `seen`, it is deleted locally

### 5) Commit checkpoint

At the very end, `history_index` is set from max observed history ID.

---

## Why initial sync can take many hours

For a giant mailbox, full sync does all of the following:

- lists essentially every message
- fetches metadata for every message
- fetches full raw bodies for uncached messages
- parses MIME
- writes each message to disk
- updates cache entries per message

Even with parallel workers, this is API- and I/O-heavy. Gmail API rate limits are enforced by `RateLimit` (`maxQps=50`) with retries.

---

## Why failure is painful to reproduce

Key behavior: `history_index` is saved **only after full sync completes successfully**.

If sync fails late:
- no incremental checkpoint is committed
- next run starts in full-sync mode again
- operator must wait through long re-scan/reprocessing

Some prior work may still exist in cache/maildir (so not every message is re-downloaded), but the run still has to go through large mailbox traversal and metadata work again.

---

## Incremental sync behavior (steady state)

`incremental(historyIndex)` uses Gmail History API:

- Fetch history pages since cached `history_index`
- Convert history records into ops:
  - message add
  - message delete
  - label changes
- Shard by message ID so events for the same message stay ordered
- Apply ops, then commit newest `history_index`

If Gmail returns 404 for the initial history request (expired token), sync falls back to full sync.

---

## Concurrency + ordering

- Worker concurrency is configurable (`--parallel`)
- Channel buffers configurable (`--buffer`)
- Incremental mode shards history events by message ID (`id % ConcurrentDownloads`) to preserve per-message event order

---

## Known operational risk in retry/backoff

In `lib/ratelimit.go`, backoff sleep is computed as:

```go
s := time.Duration(math.Pow(float64(r.BackoffStart.Nanoseconds()), float64(i)))
```

This is an unusual formula (nanoseconds raised to power `i`) and can grow extremely large, causing very long sleeps after repeated rate-limit errors. In practice this can make sync appear stalled for long periods.

---

## Summary

- Initial sync is a full mailbox materialization + metadata reconciliation pipeline.
- It is naturally slow on large inboxes because it performs broad API + disk work.
- Failures are expensive because checkpoint commit (`history_index`) is end-of-run, so partial progress does not become a resumable incremental starting point.
