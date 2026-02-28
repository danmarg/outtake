# V2 Milestone 2 Plan — Downloading-Archived

## Goal
Materialize message bodies into Maildir from the message IDs already captured by Milestone 1.

## Non-goals
- No history-based incremental sync yet.
- No major schema redesign in this milestone.

---

## Task 1: Define phase-2 cursor

Files:
- `lib/gmail/list_pages_schema.go`
- `lib/gmail/list_pages.go` (or new `lib/gmail/message_sync.go`)
- `lib/gmail/*_test.go`

- [ ] Add sync-state keys for body materialization checkpoint:
  - `sync.materialize.cursor.response_id`
  - `sync.materialize.cursor.message_id`
- [ ] Add helpers to read/write these keys atomically.

## Task 2: Deterministic message stream from M1 tables

Files:
- `lib/gmail/message_sync.go` (new)
- `lib/gmail/*_test.go`

- [ ] Define stable ordered stream from stored list rows.
  - Proposed order: `responseId DESC`, then message insertion order within response.
- [ ] Add SQL helper to fetch next N IDs after checkpoint.

## Task 3: Fetch raw + deliver to Maildir

Files:
- `lib/gmail/message_sync.go`
- `lib/maildir/*`
- `lib/gmail/service.go`

- [ ] For each message ID:
  - fetch raw via Gmail API,
  - parse/normalize as needed,
  - deliver to Maildir,
  - advance checkpoint only after success.
- [ ] Continue on per-message errors; log and move forward.

## Task 4: Resume/crash behavior

Files:
- `lib/gmail/message_sync.go`
- `lib/gmail/*_test.go`

- [ ] On restart, continue from checkpoint (roll-forward semantics).
- [ ] If checkpoint message not found, advance to next available row.
- [ ] Add crash-restart test proving forward progress.

## Task 5: Performance logging (phase 2)

Files:
- `lib/gmail/message_sync.go`
- `main.go`

- [ ] Emit periodic performance logs during message download:
  - downloaded count
  - throughput (msg/s)
  - latency (s/msg)
  - error count / retry count
- [ ] Include start/resume position and completion summary (`downloaded`, `skipped`, `failed`, elapsed).
- [ ] Keep logs readable and clearly separate phase-1 vs phase-2 prefixes.

## Task 6: Wire into default sync

Files:
- `main.go`
- `lib/gmail/gmail.go`

- [ ] Run Phase 1 list sync first.
- [ ] Run Phase 2 message sync immediately after.
- [ ] Ensure phase-2 progress logger is enabled by default.

## Task 7: Verification

- [ ] Focused tests for phase 2.
- [ ] Full suite: `go test ./...`
- [ ] Manual smoke: run sync twice and confirm second run mostly skips/advances with minimal work.
