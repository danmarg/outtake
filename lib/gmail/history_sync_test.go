package gmail

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
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

func TestSyncHistoryWithDBAppliesLabelDeltaFromSQLiteState(t *testing.T) {
	g, svc, dir := getTestClient()
	db := openTestDB(t, filepath.Join(dir, "history_labels.db"))
	defer db.Close()
	if err := ensureListPagesSchema(db); err != nil {
		t.Fatal(err)
	}
	seedListRows(t, db, []listedMessage{{MessageID: "m1"}})
	if _, err := db.Exec(`INSERT INTO gmail_labels(id, name, type, updatedAtMs) VALUES('Label_1','One','user',1),('Label_2','Two','user',1)`); err != nil {
		t.Fatal(err)
	}
	raw := base64.URLEncoding.EncodeToString([]byte("From: a@b\nTo: c@d\nSubject: hi\n\nbody"))
	svc.Msgs["m1"] = raw
	svc.Metadata["m1"] = &gmailapi.Message{Id: "m1", LabelIds: []string{"Label_1"}}
	if err := g.SyncListedMessagesWithDB(db); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`INSERT INTO sync_state(key,value,updatedAtMs) VALUES(?,?,1)`, syncStateHistoryCursorCommitted, "100"); err != nil {
		t.Fatal(err)
	}
	svc.History[""] = &gmailapi.ListHistoryResponse{History: []*gmailapi.History{{Id: 105, LabelsAdded: []*gmailapi.HistoryLabelAdded{{Message: &gmailapi.Message{Id: "m1"}, LabelIds: []string{"Label_2"}}}}}}

	if err := g.SyncHistoryWithDB(db); err != nil {
		t.Fatal(err)
	}
	fn, err := g.dir.GetFile(messageMaildirKey("m1"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(fn)
	if err != nil {
		t.Fatal(err)
	}
	txt := string(b)
	if !strings.Contains(txt, "X-Keywords: One") || !strings.Contains(txt, "X-Keywords: Two") {
		t.Fatalf("expected mapped labels One+Two in headers")
	}
}
