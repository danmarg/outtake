package gmail

import (
	"database/sql"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
	gmailapi "google.golang.org/api/gmail/v1"
)

func seedListRows(t *testing.T, db *sql.DB, rows []listedMessage) {
	t.Helper()
	if err := ensureListPagesSchema(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gmail_users_messages_list_requests(pageToken, requestedAtMs, nextPageToken, resultSizeEstimate, rawJson) VALUES('',1,'',0,'{}')`); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO gmail_users_messages_list_responses(requestId, nextPageToken, resultSizeEstimate, receivedAtMs, rawJson) VALUES(1,'',0,1,'{}')`)
	if err != nil {
		t.Fatal(err)
	}
	defaultRespID, _ := res.LastInsertId()
	for _, r := range rows {
		respID := r.ResponseID
		if respID == 0 {
			respID = defaultRespID
		}
		if _, err := db.Exec(`INSERT INTO gmail_users_messages_list_response_messages(responseId, id, threadId, rawJson) VALUES(?, ?, ?, '{}')`, respID, r.MessageID, r.MessageID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSyncListedMessagesWritesToMaildir(t *testing.T) {
	g, svc, dir := getTestClient()
	dbPath := filepath.Join(dir, "m2.db")
	db := openTestDB(t, dbPath)
	seedListRows(t, db, []listedMessage{{MessageID: "m1"}, {MessageID: "m2"}})
	db.Close()

	raw := base64.URLEncoding.EncodeToString([]byte("From: a@b\nTo: c@d\nSubject: hi\n\nbody"))
	svc.Msgs["m1"] = raw
	svc.Msgs["m2"] = raw
	svc.Metadata["m1"] = &gmailapi.Message{Id: "m1", LabelIds: []string{"INBOX"}}
	svc.Metadata["m2"] = &gmailapi.Message{Id: "m2", LabelIds: []string{"STARRED"}}

	if err := g.SyncListedMessages(dbPath); err != nil {
		t.Fatalf("SyncListedMessages() error = %v", err)
	}

	files, err := os.ReadDir(filepath.Join(dir, "new"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("maildir new files=%d expected 2", len(files))
	}
}

func TestSyncListedMessagesResumesFromCheckpoint(t *testing.T) {
	g, svc, dir := getTestClient()
	dbPath := filepath.Join(dir, "m2_resume.db")
	db := openTestDB(t, dbPath)
	if err := ensureListPagesSchema(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gmail_users_messages_list_requests(pageToken, requestedAtMs, nextPageToken, resultSizeEstimate, rawJson) VALUES('',1,'',0,'{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gmail_users_messages_list_responses(id, requestId, nextPageToken, resultSizeEstimate, receivedAtMs, rawJson) VALUES(1,1,'',0,1,'{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gmail_users_messages_list_responses(id, requestId, nextPageToken, resultSizeEstimate, receivedAtMs, rawJson) VALUES(2,1,'',0,1,'{}')`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if _, err := db.Exec(`INSERT INTO gmail_users_messages_list_response_messages(responseId, id, threadId, rawJson) VALUES(1, ?, ?, '{}')`, id, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO gmail_users_messages_list_response_messages(responseId, id, threadId, rawJson) VALUES(2, 'z', 'z', '{}')`); err != nil {
		t.Fatal(err)
	}
	tx, _ := db.Begin()
	if err := setMaterializeCheckpoint(tx, 1, "b"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	db.Close()

	raw := base64.URLEncoding.EncodeToString([]byte("From: a@b\nTo: c@d\nSubject: hi\n\nbody"))
	svc.Msgs["c"] = raw
	svc.Msgs["z"] = raw
	svc.Metadata["c"] = &gmailapi.Message{Id: "c"}
	svc.Metadata["z"] = &gmailapi.Message{Id: "z"}

	if err := g.SyncListedMessages(dbPath); err != nil {
		t.Fatalf("SyncListedMessages() error = %v", err)
	}

	if _, err := g.dir.GetFile(stableArchiveKey(4, 3, "c")); err != nil {
		t.Fatalf("expected message c to be synced: %v", err)
	}
	if _, err := g.dir.GetFile(stableArchiveKey(4, 4, "z")); err != nil {
		t.Fatalf("expected message z to be synced: %v", err)
	}
	if _, err := g.dir.GetFile(stableArchiveKey(4, 1, "a")); err == nil {
		t.Fatalf("did not expect message a to be re-synced")
	}
}
