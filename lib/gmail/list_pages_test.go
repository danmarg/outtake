package gmail

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
	gmailapi "google.golang.org/api/gmail/v1"
	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T, p string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEnsureListPagesSchema(t *testing.T) {
	_, _, dir := getTestClient()
	dbPath := filepath.Join(dir, "schema.db")
	db := openTestDB(t, dbPath)
	defer db.Close()
	if err := ensureListPagesSchema(db); err != nil {
		t.Fatalf("ensureListPagesSchema() error = %v", err)
	}
	for _, tbl := range []string{
		"gmail_users_messages_list_requests",
		"gmail_users_messages_list_responses",
		"gmail_users_messages_list_response_messages",
		"gmail_users_messages_index",
		"oauth_tokens",
		"sync_state",
		"gmail_labels",
	} {
		if countRows(t, db, "sqlite_schema WHERE type='table' AND name='"+tbl+"'") != 1 {
			t.Fatalf("expected table %s to exist", tbl)
		}
	}
}

func TestGetResumePageTokenFromMaxRequestID(t *testing.T) {
	_, _, dir := getTestClient()
	dbPath := filepath.Join(dir, "resume.db")
	db := openTestDB(t, dbPath)
	defer db.Close()
	if err := ensureListPagesSchema(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gmail_users_messages_list_requests(pageToken, requestedAtMs, nextPageToken, resultSizeEstimate, rawJson) VALUES('a', 1, 'p2', 2, '{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gmail_users_messages_list_requests(pageToken, requestedAtMs, nextPageToken, resultSizeEstimate, rawJson) VALUES('b', 2, 'p3', 2, '{}')`); err != nil {
		t.Fatal(err)
	}
	tok, ok, err := getResumePageToken(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || tok != "p3" {
		t.Fatalf("getResumePageToken() = (%q, %v), expected (p3, true)", tok, ok)
	}
}

func TestSyncListPagesPersistsOAuthToken(t *testing.T) {
	g, svc, dir := getTestClient()
	dbPath := filepath.Join(dir, "oauth.db")
	g.cache.SetOauthToken(&oauth2.Token{AccessToken: "a", RefreshToken: "r", TokenType: "Bearer", Expiry: time.Unix(10, 0)})
	svc.Messages[""] = &gmailapi.ListMessagesResponse{}
	db2 := openTestDB(t, dbPath)
	defer db2.Close()
	if err := g.SyncListPagesWithDB(db2); err != nil {
		t.Fatalf("SyncListPagesWithDB() error = %v", err)
	}
	db := openTestDB(t, dbPath)
	defer db.Close()
	if got := countRows(t, db, "oauth_tokens"); got != 1 {
		t.Fatalf("oauth token rows = %d, expected 1", got)
	}
}

func TestSyncListPagesStoresAllPages(t *testing.T) {
	g, svc, dir := getTestClient()
	dbPath := filepath.Join(dir, "list-pages.db")

	svc.Messages[""] = &gmailapi.ListMessagesResponse{
		Messages:           []*gmailapi.Message{{Id: "m1", ThreadId: "t1"}},
		NextPageToken:      "p2",
		ResultSizeEstimate: 2,
	}
	svc.Messages["p2"] = &gmailapi.ListMessagesResponse{
		Messages:           []*gmailapi.Message{{Id: "m2", ThreadId: "t2"}},
		NextPageToken:      "",
		ResultSizeEstimate: 2,
	}

	db2 := openTestDB(t, dbPath)
	defer db2.Close()
	if err := g.SyncListPagesWithDB(db2); err != nil {
		t.Fatalf("SyncListPagesWithDB() error = %v", err)
	}

	db := openTestDB(t, dbPath)
	defer db.Close()
	if got := countRows(t, db, "gmail_users_messages_list_requests"); got != 2 {
		t.Fatalf("request rows = %d, expected 2", got)
	}
	if done, ok, err := getSyncState(db, syncStateListDone); err != nil || !ok || done != "1" {
		t.Fatalf("sync_state done = (%q,%v,%v), expected (1,true,nil)", done, ok, err)
	}
}

func TestSyncListPagesResumesFromSyncStateCursor(t *testing.T) {
	g, svc, dir := getTestClient()
	dbPath := filepath.Join(dir, "list-pages-state.db")

	svc.Messages["p2"] = &gmailapi.ListMessagesResponse{
		Messages:           []*gmailapi.Message{{Id: "m2", ThreadId: "t2"}},
		NextPageToken:      "",
		ResultSizeEstimate: 2,
	}

	db := openTestDB(t, dbPath)
	defer db.Close()
	if err := ensureListPagesSchema(db); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := setSyncState(tx, syncStateListDone, "0"); err != nil {
		t.Fatal(err)
	}
	if err := setSyncState(tx, syncStateListNextPageToken, "p2"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db2 := openTestDB(t, dbPath)
	if err := g.SyncListPagesWithDB(db2); err != nil {
		db2.Close()
		t.Fatalf("SyncListPagesWithDB() error = %v", err)
	}
	db2.Close()

	db3 := openTestDB(t, dbPath)
	defer db3.Close()
	if got := countRows(t, db3, "gmail_users_messages_list_requests"); got != 1 {
		t.Fatalf("request rows = %d, expected 1", got)
	}
}

func TestSyncListPagesResumesFromLatestRequest(t *testing.T) {
	g, svc, dir := getTestClient()
	dbPath := filepath.Join(dir, "list-pages.db")

	svc.Messages["p2"] = &gmailapi.ListMessagesResponse{
		Messages:           []*gmailapi.Message{{Id: "m2", ThreadId: "t2"}},
		NextPageToken:      "",
		ResultSizeEstimate: 2,
	}

	// Seed existing request row with nextPageToken = p2.
	db := openTestDB(t, dbPath)
	defer db.Close()
	if err := ensureListPagesSchema(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gmail_users_messages_list_requests(pageToken, requestedAtMs, nextPageToken, resultSizeEstimate, rawJson) VALUES('', 1, 'p2', 2, '{}')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db2 := openTestDB(t, dbPath)
	if err := g.SyncListPagesWithDB(db2); err != nil {
		db2.Close()
		t.Fatalf("SyncListPagesWithDB() error = %v", err)
	}
	db2.Close()

	db3 := openTestDB(t, dbPath)
	defer db3.Close()
	if got := countRows(t, db3, "gmail_users_messages_list_requests"); got != 2 {
		t.Fatalf("request rows = %d, expected 2", got)
	}

	_ = os.Remove(dbPath)
}
