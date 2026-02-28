# V2 List Pages Milestone 1 Plan

## Goal
Default sync in v2 should:
- list all Gmail pages (`Users.Messages.List`) into SQLite,
- resume from latest stored page,
- store OAuth token in SQLite.

## Scope
- No body download.
- No history sync.
- No special CLI mode; this is the default sync path.

---

## Task 1: SQLite schema for list sync

Files:
- `lib/gmail/list_pages_schema.go`
- `lib/gmail/list_pages_test.go`
- `architecture-v2-plan.md`

- [ ] Add schema creation for:
  - `gmail_users_messages_list_requests`
  - `gmail_users_messages_list_responses`
  - `gmail_users_messages_list_response_messages`
  - `gmail_users_messages_index`
  - token table in SQLite
- [ ] Add/adjust tests to verify schema exists.
- [ ] Run: `go test ./lib/gmail -run TestEnsureListPagesSchema -v`

## Task 2: Resume cursor logic (latest request row)

Files:
- `lib/gmail/list_pages_schema.go`
- `lib/gmail/list_pages_test.go`

- [ ] Implement helper to read resume token from latest request (`max(id)`).
- [ ] Add test proving latest row wins.
- [ ] Run: `go test ./lib/gmail -run TestGetResumePageTokenFromMaxRequestID -v`

## Task 3: Implement `SyncListPages`

Files:
- `lib/gmail/list_pages.go`
- `lib/gmail/gmail.go`
- `lib/gmail/list_pages_test.go`

- [ ] Implement page loop:
  - read resume token,
  - call `Users.Messages.List` with query filter `q = "-in:chats"`,
  - persist request query (`q`) in `gmail_users_messages_list_requests`,
  - store request/response/messages in one transaction per page,
  - continue until `nextPageToken` empty.
- [ ] Add tests for:
  - multi-page storage,
  - resume from existing DB.
- [ ] Run: `go test ./lib/gmail -run TestSyncListPages -v`

## Task 4: Make milestone 1 default behavior + token in SQLite

Files:
- `main.go`
- `lib/gmail/gmail.go`
- `lib/gmail/list_pages.go`
- `README.md`

- [ ] Route default sync path to list-pages sync.
- [ ] Store/retrieve OAuth token in SQLite.
- [ ] Add tests for token persistence.
- [ ] Run: `go test ./lib/gmail -v`

## Task 5: Verify

- [ ] Run focused tests for list sync.
- [ ] Run full suite: `go test ./...`
- [ ] Manual smoke run: `go run . --directory <maildir>` and confirm DB grows page-by-page.
