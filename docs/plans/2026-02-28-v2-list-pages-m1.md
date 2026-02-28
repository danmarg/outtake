# V2 Milestone 1 Plan (Minimal Page Cursor)

## Goal
Keep SQLite minimal:
- store list-page resume cursor,
- store OAuth token,
- do not store per-message corpus in SQLite.

Maildir remains the message store.

## Minimal schema

- `sync_state(key, value, updatedAtMs)`
- `oauth_tokens(account, tokenType, accessToken, refreshToken, expiryUnixMs, scope, rawJson, updatedAtMs)`

## Cursor keys

- `users.messages.list.nextPageToken`
- `users.messages.list.done`

---

## Task 1: Remove unneeded list corpus schemas

Files:
- `lib/gmail/list_pages_schema.go`
- `lib/gmail/list_pages_test.go`

- [ ] Remove schema creation for:
  - `gmail_users_messages_list_requests`
  - `gmail_users_messages_list_responses`
  - `gmail_users_messages_list_response_messages`
  - `gmail_users_messages_index`
- [ ] Keep only `sync_state` and `oauth_tokens`.
- [ ] Update schema tests accordingly.

## Task 2: Switch resume logic to `sync_state` cursor

Files:
- `lib/gmail/list_pages_schema.go`
- `lib/gmail/list_pages.go`
- `lib/gmail/list_pages_test.go`

- [ ] Replace "latest request row" resume logic with `sync_state['users.messages.list.nextPageToken']`.
- [ ] Mark done with `sync_state['users.messages.list.done']='1'` when token is empty.
- [ ] Add tests for cursor-based resume and completion handling.

## Task 3: Keep list phase operational with minimal persistence

Files:
- `lib/gmail/list_pages.go`

- [ ] Keep paging loop and logging.
- [ ] Persist only cursor updates per page (no response/message row inserts).
- [ ] Ensure oauth token persistence still works.

## Task 4: Verify

- [ ] Run focused tests: `go test ./lib/gmail -v`
- [ ] Run full suite: `go test ./...`
- [ ] Manual smoke run: `go run . --directory <maildir>` and confirm cursor advances in SQLite.
