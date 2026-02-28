# V2 List Pages Milestone 1 Plan

## Goal
Default v2 sync should:
- list Gmail pages (`Users.Messages.List`) into SQLite,
- resume from latest stored page chain,
- store OAuth token in SQLite,
- run as default sync path.

## Scope
- No body download.
- No history sync.

---

## Task 1: SQLite schema for list sync

Files:
- `lib/gmail/list_pages_schema.go`
- `lib/gmail/list_pages_test.go`

- [x] Add schema creation for:
  - `gmail_users_messages_list_requests`
  - `gmail_users_messages_list_responses`
  - `gmail_users_messages_list_response_messages`
  - `gmail_users_messages_index`
  - `oauth_tokens`
  - `sync_state`
- [x] Add tests to verify schema exists.
- [x] Run: `go test ./lib/gmail -run TestEnsureListPagesSchema -v`

## Task 2: Resume cursor logic (latest request row)

Files:
- `lib/gmail/list_pages_schema.go`
- `lib/gmail/list_pages_test.go`

- [x] Implement helper to read resume token from latest request (`max(id)` semantics via latest row).
- [x] Add test proving latest row wins.
- [x] Run: `go test ./lib/gmail -run TestGetResumePageTokenFromMaxRequestID -v`

## Task 3: Implement `SyncListPages`

Files:
- `lib/gmail/list_pages.go`
- `lib/gmail/list_pages_test.go`

- [x] Implement page loop:
  - read resume token,
  - call `Users.Messages.List`,
  - store request/response/messages in one transaction per page,
  - continue until `nextPageToken` empty.
- [x] Add tests for:
  - multi-page storage,
  - resume from existing DB.
- [x] Run: `go test ./lib/gmail -run TestSyncListPages -v`
- [ ] TODO: apply `q="-in:chats"` consistently and persist query value as planned.

## Task 4: Make milestone 1 default behavior + token in SQLite

Files:
- `main.go`
- `lib/gmail/list_pages.go`

- [x] Route default sync path to list-pages sync.
- [x] Persist OAuth token in SQLite.
- [x] Add test for token persistence.
- [x] Run: `go test ./lib/gmail -v`

## Task 5: Verification

- [x] Run focused tests for list sync.
- [x] Run full suite: `go test ./...`
- [x] Manual smoke observed: sync can report completed state from existing page chain.
