# V2 Milestone 3.1 Plan — History Label Updates Without Bolt

## Goal
Make history-based label updates reliable for V2-synced messages by removing Bolt-cache dependence from the M3 label path.

## Working Notes
This file is the live plan/checklist and should be updated as tasks are started/completed.

---

## Checklist

- [ ] Standardize filename scheme to Gmail message ID
  - [ ] Change M2 Downloading-Archived writes to use `<gmailMessageId>.mail`
  - [ ] Change M3 `messagesAdded` writes to use the same `<gmailMessageId>.mail`
  - [ ] Keep resume/idempotency based on file existence for that exact filename

- [ ] Add retrofit script for existing Maildir filenames
  - [ ] Scan `new/` and `cur/`
  - [ ] Rename stable legacy keys to `<gmailMessageId>.mail` where message ID can be derived
  - [ ] Dry-run mode + summary output

- [ ] Add SQLite label-state table
  - [ ] `gmail_message_labels(messageId, label, updatedAtMs, PRIMARY KEY(messageId,label))`
  - [ ] Schema test coverage

- [ ] Retrofit/migration strategy (no special label backfill)
  - [ ] Provide DB reset script for new M3.1 state tables (drop/recreate or truncate)
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
  - [ ] Rewrite message file in place via Maildir-safe replace flow

- [ ] Keep unknown-label handling minimal and correct
  - [ ] Reuse existing SQLite label-name mapping
  - [ ] Lazy refresh once on unknown ID
  - [ ] Fallback to raw label ID if still unresolved

- [ ] Logging/observability
  - [ ] Add counters: `labels_applied`, `labels_missing_file`, `labels_missing_state`, `labels_failed`
  - [ ] One-time explanatory note for missing file/state

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
