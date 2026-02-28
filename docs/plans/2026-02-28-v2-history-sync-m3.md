# V2 History Sync Milestone 3 Plan

## Goal
Add history-based incremental sync so v2 can stay up to date efficiently after list/body backfill.

## Scope
- Use `Users.History.List` with durable cursor state in SQLite.
- Reuse existing message/materialization and maildir write paths where possible.
- Keep fallback behavior explicit when history cursor expires.

---

## Task 1: Sync-state keys for history

Files:
- `lib/gmail/list_pages_schema.go`
- `lib/gmail/*_test.go`

- [ ] Add/standardize history keys in `sync_state`:
  - `users.history.list.cursor`
  - `users.history.list.cursor_progress`
  - `users.history.list.pageToken` (optional, for page-level resume)
- [ ] Add helpers to read/write/clear these keys.

## Task 2: Bootstrap cursor when missing

Files:
- `lib/gmail/message_sync.go`
- `lib/gmail/history_sync.go` (new)

- [ ] If no committed history cursor exists, derive one once from available message metadata `historyId`.
- [ ] Persist derived cursor and log source.
- [ ] If no usable `historyId`, log and skip M3 for that run.

## Task 3: Implement `SyncHistory(dbPath)`

Files:
- `lib/gmail/history_sync.go` (new)
- `lib/gmail/service.go`

- [ ] Call `Users.History.List(startHistoryId=cursor, pageToken=...)`.
- [ ] Iterate pages until complete.
- [ ] Persist in-flight progress (`cursor_progress`, `pageToken`) during run.

## Task 4: Apply history events

Files:
- `lib/gmail/history_sync.go`
- `lib/gmail/gmail.go`
- `lib/maildir/*`

- [ ] `messagesAdded`: materialize new messages (reuse phase2/download path).
- [ ] `messagesDeleted`: delete local message if present.
- [ ] `labelsAdded/labelsRemoved`: update local labels/header best-effort.

## Task 5: Commit and recovery semantics

Files:
- `lib/gmail/history_sync.go`

- [ ] On successful completion:
  - promote `cursor_progress` to committed `cursor`,
  - clear progress/page token keys.
- [ ] On interruption/error:
  - keep progress keys for resume,
  - do not advance committed cursor past unapplied work.

## Task 6: Expired cursor fallback

Files:
- `lib/gmail/history_sync.go`
- `lib/gmail/gmail.go`

- [ ] Handle Gmail 404 expired history cursor explicitly.
- [ ] Clear history cursor state.
- [ ] Fall back to M1+M2 backfill path on same run.

## Task 7: Performance logging

Files:
- `lib/gmail/history_sync.go`
- `main.go`

- [ ] Emit periodic perf logs:
  - events processed,
  - added/deleted/label-updated counts,
  - msg/s and elapsed,
  - current cursor/page token state.
- [ ] Emit completion summary.

## Task 8: Wire into default flow

Files:
- `main.go`
- `lib/gmail/gmail.go`

- [ ] Default run sequence:
  1. M1 list pages
  2. M2 materialize listed messages
  3. M3 history sync

## Task 9: Verification

- [ ] Focused tests for M3 logic (happy path, resume, expired cursor fallback, idempotent replay).
- [ ] Full suite: `go test ./...`
- [ ] Manual smoke: run twice and confirm second run mostly processes only deltas.
