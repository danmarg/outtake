package gmail

import (
	"path/filepath"
	"testing"

	gmailapi "google.golang.org/api/gmail/v1"
)

func TestSyncHistoryWithDBCommitsCursor(t *testing.T) {
	g, svc, dir := getTestClient()
	db := openTestDB(t, filepath.Join(dir, "history.db"))
	defer db.Close()
	if err := ensureListPagesSchema(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sync_state(key,value,updatedAtMs) VALUES(?,?,1)`, syncStateHistoryCursorCommitted, "100"); err != nil {
		t.Fatal(err)
	}

	svc.History[""] = &gmailapi.ListHistoryResponse{
		History: []*gmailapi.History{{Id: 110}},
	}

	if err := g.SyncHistoryWithDB(db); err != nil {
		t.Fatal(err)
	}
	v, ok, err := getSyncState(db, syncStateHistoryCursorCommitted)
	if err != nil || !ok {
		t.Fatalf("committed cursor missing: %v %v", ok, err)
	}
	if v != "110" {
		t.Fatalf("committed cursor=%s expected 110", v)
	}
}

func TestSyncHistoryWithDBBootstrapsCursor(t *testing.T) {
	g, svc, dir := getTestClient()
	db := openTestDB(t, filepath.Join(dir, "history_bootstrap.db"))
	defer db.Close()
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
	if _, err := db.Exec(`INSERT INTO gmail_users_messages_list_response_messages(responseId, id, threadId, rawJson) VALUES(1, 'newer', 't1', '{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gmail_users_messages_list_response_messages(responseId, id, threadId, rawJson) VALUES(2, 'older', 't2', '{}')`); err != nil {
		t.Fatal(err)
	}
	svc.Metadata["newer"] = &gmailapi.Message{Id: "newer", HistoryId: 200}
	svc.Metadata["older"] = &gmailapi.Message{Id: "older", HistoryId: 100}
	svc.History[""] = &gmailapi.ListHistoryResponse{}

	if err := g.SyncHistoryWithDB(db); err != nil {
		t.Fatal(err)
	}
	v, ok, err := getSyncState(db, syncStateHistoryCursorCommitted)
	if err != nil || !ok {
		t.Fatalf("committed cursor missing: %v %v", ok, err)
	}
	if v != "200" {
		t.Fatalf("committed cursor=%s expected 200", v)
	}
}
