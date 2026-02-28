package gmail

import "database/sql"

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
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func getResumePageToken(db *sql.DB) (string, bool, error) {
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
