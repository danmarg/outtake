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
	if _, err := db.Exec(`INSERT INTO gmail_users_messages_index(id, threadId, lastResponseId, updatedAtMs, rawJson) VALUES('m1','t1',1,1,'{}')`); err != nil {
		t.Fatal(err)
	}
	svc.Metadata["m1"] = &gmailapi.Message{Id: "m1", HistoryId: 200}
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
