# V2 Milestone 3.1 Plan — History Label Updates Without Bolt

## Goal
Make history-based label updates reliable for V2-synced messages by removing Bolt-cache dependence from the M3 label path.

## Non-goals
- No legacy label backfill by parsing old headers/old DB.
- No dual-write compatibility layer for old and new label-state paths.

## Working Notes
This file is the live plan/checklist and should be updated as tasks are started/completed.

---

## Checklist

- [ ] Standardize filename scheme to Gmail message ID
  - [ ] Change M2 Downloading-Archived writes to use `<gmailMessageId>.mail`
  - [ ] Change M3 `messagesAdded` writes to use the same `<gmailMessageId>.mail`
  - [ ] Update skip/idempotency checks to file existence of exactly `<gmailMessageId>.mail`

- [ ] Add retrofit script for existing Maildir filenames
  - [ ] Scan `new/` and `cur/`
  - [ ] Rename stable legacy keys to `<gmailMessageId>.mail` where message ID can be derived
  - [ ] Collision policy: if target `<gmailMessageId>.mail` exists, skip and report
  - [ ] Dry-run mode + summary output

- [ ] Add SQLite label-state table
  - [ ] `gmail_message_labels(messageId, label, updatedAtMs, PRIMARY KEY(messageId,label))`
  - [ ] Schema test coverage

- [ ] Retrofit/migration strategy (no special label backfill)
  - [ ] Provide DB reset script for new M3.1 state tables (drop/recreate or truncate)
  - [ ] Explicit reset scope: `gmail_message_labels` and other M3.1-introduced state tables only
  - [ ] Preserve existing list corpus and core sync_state keys needed for normal resume
  - [ ] Do **not** bootstrap labels from legacy headers/old DB
  - [ ] Rebuild label state through normal sync flow (M1 -> M2 -> M3)

- [ ] Persist initial full label set on add/materialize
  - [ ] M2 add path updates `gmail_message_labels`
  - [ ] M3 add path updates `gmail_message_labels`

- [ ] Replace cache-based M3 label updates
  - [ ] Stop using `computeLabels + writeLabels` in M3 path
  - [ ] Apply label deltas in SQL (`gmail_message_labels`)
  - [ ] Locate message file by deterministic `<gmailMessageId>.mail` in `new/` or `cur/`
  - [ ] Re-render `X-Keywords` from SQL state + `gmail_labels` name mapping
  - [ ] Rewrite message using Maildir-safe replace flow (write temp/new message, atomically switch, remove old file)

- [ ] Keep unknown-label handling minimal and correct
  - [ ] Reuse existing SQLite label-name mapping
  - [ ] Lazy refresh once on unknown ID
  - [ ] Fallback to raw label ID if still unresolved

- [ ] Logging/observability
  - [ ] Add counters: `labels_applied`, `labels_missing_file`, `labels_missing_state`, `labels_failed`
  - [ ] Use stable log prefix `history:` and include these counters in periodic perf logs
  - [ ] One-time explanatory note for missing file/state

- [ ] Acceptance criteria
  - [ ] After migration + one full run, next run does near-zero re-downloads in M2
  - [ ] History label change updates `X-Keywords` without Bolt cache state
  - [ ] No data-loss events from filename migration (collisions are skipped and reported)

- [ ] Rollback
  - [ ] Filename migration supports dry-run first and report-only mode
  - [ ] On partial rename failure, script reports completed vs failed paths for operator rollback/retry

- [ ] Verification
  - [ ] Unit tests for label add/remove updates without Bolt
  - [ ] Unit test for unknown-ID refresh
  - [ ] Unit test for missing-file non-fatal behavior
  - [ ] End-to-end test: M2 add then M3 label change updates headers
  - [ ] `go test ./...`

---

## Progress Log
- [x] Initial plan captured.
- [x] Plan updated: standardized filename target is `<gmailMessageId>.mail` for both M2 and M3.
- [ ] Implementation started.
