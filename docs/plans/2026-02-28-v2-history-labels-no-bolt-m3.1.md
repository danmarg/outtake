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

- [x] Standardize filename scheme to Gmail message ID
  - [x] Change M2 Downloading-Archived writes to use `<gmailMessageId>.mail`
  - [x] Change M3 `messagesAdded` writes to use the same `<gmailMessageId>.mail`
  - [x] Update skip/idempotency checks to file existence of exactly `<gmailMessageId>.mail`

- [x] Add retrofit script for existing Maildir filenames
  - [x] Scan `new/` and `cur/`
  - [x] Rename stable legacy keys to `<gmailMessageId>.mail` where message ID can be derived
  - [x] Collision policy: if target `<gmailMessageId>.mail` exists, skip and report
  - [x] Dry-run mode + summary output

- [x] Add SQLite label-state table
  - [x] `gmail_message_labels(messageId, label, updatedAtMs, PRIMARY KEY(messageId,label))`
  - [x] Schema test coverage

- [x] Retrofit/migration strategy (no special label backfill)
  - [x] Provide DB reset script for new M3.1 state tables (drop/recreate or truncate)
  - [x] Explicit reset scope: `gmail_message_labels` and other M3.1-introduced state tables only
  - [x] Preserve existing list corpus and core sync_state keys needed for normal resume
  - [x] Do **not** bootstrap labels from legacy headers/old DB
  - [x] Rebuild label state through normal sync flow (M1 -> M2 -> M3)

- [x] Persist initial full label set on add/materialize
  - [x] M2 add path updates `gmail_message_labels`
  - [x] M3 add path updates `gmail_message_labels`

- [x] Replace cache-based M3 label updates
  - [x] Stop using `computeLabels + writeLabels` in M3 path
  - [x] Apply label deltas in SQL (`gmail_message_labels`)
  - [x] Locate message file by deterministic `<gmailMessageId>.mail` in `new/` or `cur/`
  - [x] Re-render `X-Keywords` from SQL state + `gmail_labels` name mapping
  - [x] Rewrite message using Maildir-safe replace flow (write temp/new message, atomically switch, remove old file)

- [x] Keep unknown-label handling minimal and correct
  - [x] Reuse existing SQLite label-name mapping
  - [x] Lazy refresh once on unknown ID
  - [x] Fallback to raw label ID if still unresolved

- [x] Logging/observability
  - [x] Add counters: `labels_applied`, `labels_missing_file`, `labels_missing_state`, `labels_failed`
  - [x] Use stable log prefix `history:` and include these counters in periodic perf logs
  - [ ] One-time explanatory note for missing file/state

- [ ] Acceptance criteria
  - [ ] After migration + one full run, next run does near-zero re-downloads in M2
  - [x] History label change updates `X-Keywords` without Bolt cache state
  - [x] No data-loss events from filename migration (collisions are skipped and reported)

- [x] Rollback
  - [x] Filename migration supports dry-run first and report-only mode
  - [x] On partial rename failure, script reports completed vs failed paths for operator rollback/retry

- [x] Verification
  - [x] Unit tests for label add/remove updates without Bolt
  - [x] Unit test for unknown-ID refresh
  - [x] Unit test for missing-file non-fatal behavior
  - [x] End-to-end test: M2 add then M3 label change updates headers
  - [x] `go test ./...`

---

## Progress Log
- [x] Initial plan captured.
- [x] Plan updated: standardized filename target is `<gmailMessageId>.mail` for both M2 and M3.
- [x] Implementation started.
- [x] Implemented filename standardization in M2/M3 and SQL-backed M3 label delta path.
- [x] Added retrofit scripts for filename migration and label-state reset.
- [ ] Add one-time explanatory log for missing file/state in history label handling.
