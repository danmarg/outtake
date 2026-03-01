package gmail

import (
	"database/sql"
	"strconv"
	"time"
)

const (
	syncStateListNextPageToken           = "sync.list.next_page_token"
	syncStateListDone                    = "sync.list.done"
	syncStateMaterializeCursorResponseID = "sync.materialize.cursor.response_id"
	syncStateMaterializeCursorMessageID  = "sync.materialize.cursor.message_id"
	syncStateMaterializeTotalMessages    = "sync.materialize.total_messages"
	syncStateHistoryCursorCommitted      = "sync.history.cursor.committed"
	syncStateHistoryCursorProgress       = "sync.history.cursor.progress"
	syncStateHistoryPageToken            = "sync.history.page_token"
)

func ensureListPagesSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS gmail_users_messages_list_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pageToken TEXT,
			labelIdsJson TEXT,
			q TEXT,
			maxResults INTEGER,
			requestedAtMs INTEGER NOT NULL,
			nextPageToken TEXT,
			resultSizeEstimate INTEGER,
			rawJson TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS gmail_users_messages_list_responses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			requestId INTEGER NOT NULL REFERENCES gmail_users_messages_list_requests(id),
			nextPageToken TEXT,
			resultSizeEstimate INTEGER,
			receivedAtMs INTEGER NOT NULL,
			rawJson TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS gmail_users_messages_list_response_messages (
			responseId INTEGER NOT NULL REFERENCES gmail_users_messages_list_responses(id),
			id TEXT NOT NULL,
			threadId TEXT,
			rawJson TEXT NOT NULL,
			PRIMARY KEY (responseId, id)
		)`,
		`CREATE TABLE IF NOT EXISTS gmail_users_messages_index (
			id TEXT PRIMARY KEY,
			threadId TEXT,
			lastResponseId INTEGER NOT NULL REFERENCES gmail_users_messages_list_responses(id),
			updatedAtMs INTEGER NOT NULL,
			rawJson TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS oauth_tokens (
			account TEXT PRIMARY KEY,
			tokenType TEXT,
			accessToken TEXT,
			refreshToken TEXT,
			expiryUnixMs INTEGER,
			scope TEXT,
			rawJson TEXT NOT NULL,
			updatedAtMs INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sync_state (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updatedAtMs INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS gmail_labels (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT,
			updatedAtMs INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS gmail_message_labels (
			messageId TEXT NOT NULL,
			label TEXT NOT NULL,
			updatedAtMs INTEGER NOT NULL,
			PRIMARY KEY (messageId, label)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func getResumePageToken(db *sql.DB) (string, bool, error) {
	if done, ok, err := getSyncState(db, syncStateListDone); err != nil {
		return "", false, err
	} else if ok && done == "1" {
		return "", true, nil
	}
	if tok, ok, err := getSyncState(db, syncStateListNextPageToken); err != nil {
		return "", false, err
	} else if ok {
		return tok, true, nil
	}

	// backward-compat fallback to request-chain resume
	var token sql.NullString
	err := db.QueryRow(`SELECT nextPageToken FROM gmail_users_messages_list_requests ORDER BY id DESC LIMIT 1`).Scan(&token)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if token.Valid {
		return token.String, true, nil
	}
	return "", true, nil
}

func getSyncState(db *sql.DB, key string) (string, bool, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM sync_state WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func setSyncState(tx *sql.Tx, key, value string) error {
	_, err := tx.Exec(`INSERT INTO sync_state(key, value, updatedAtMs)
		VALUES(?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updatedAtMs=excluded.updatedAtMs`,
		key, value, time.Now().UnixMilli())
	return err
}

func getMaterializeCheckpoint(db *sql.DB) (int64, string, error) {
	respRaw, ok, err := getSyncState(db, syncStateMaterializeCursorResponseID)
	if err != nil {
		return 0, "", err
	}
	if !ok {
		// backward-compat with earlier temporary key naming
		respRaw, ok, err = getSyncState(db, "materialize.lastResponseId")
	}
	if err != nil {
		return 0, "", err
	}
	if !ok {
		return 0, "", nil
	}
	respID, err := strconv.ParseInt(respRaw, 10, 64)
	if err != nil {
		return 0, "", err
	}
	msgID, okMsg, err := getSyncState(db, syncStateMaterializeCursorMessageID)
	if err != nil {
		return 0, "", err
	}
	if !okMsg {
		msgID, _, err = getSyncState(db, "materialize.lastMessageId")
		if err != nil {
			return 0, "", err
		}
	}
	return respID, msgID, nil
}

func setMaterializeCheckpoint(tx *sql.Tx, responseID int64, messageID string) error {
	if err := setSyncState(tx, syncStateMaterializeCursorResponseID, strconv.FormatInt(responseID, 10)); err != nil {
		return err
	}
	return setSyncState(tx, syncStateMaterializeCursorMessageID, messageID)
}
