package gmail

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"golang.org/x/oauth2"
	_ "modernc.org/sqlite"
)

func (g *Gmail) SyncListPages(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		return err
	}

	if err := ensureListPagesSchema(db); err != nil {
		return err
	}

	if tok, ok := g.cache.GetOauthToken(); ok {
		if err := persistOAuthToken(db, "me", tok); err != nil {
			return err
		}
	}

	pageToken, has, err := getResumePageToken(db)
	if err != nil {
		return err
	}
	if has && pageToken == "" {
		log.Println("listing: already complete (latest request has empty nextPageToken)")
		return nil
	}
	if has {
		log.Printf("listing: resuming from pageToken=%q", pageToken)
	} else {
		log.Println("listing: starting from first page")
	}

	pages := 0
	msgs := 0
	for {
		r, err := g.svc.GetMessages(g.labelId, pageToken)
		if err != nil {
			return err
		}
		next := r.NextPageToken
		now := time.Now().UnixMilli()
		rawResp, _ := json.Marshal(r)

		tx, err := db.Begin()
		if err != nil {
			return err
		}

		res, err := tx.Exec(`INSERT INTO gmail_users_messages_list_requests(pageToken, labelIdsJson, q, maxResults, requestedAtMs, nextPageToken, resultSizeEstimate, rawJson) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
			pageToken, "", "", nil, now, next, r.ResultSizeEstimate, string(rawResp))
		if err != nil {
			tx.Rollback()
			return err
		}
		requestID, err := res.LastInsertId()
		if err != nil {
			tx.Rollback()
			return err
		}

		res, err = tx.Exec(`INSERT INTO gmail_users_messages_list_responses(requestId, nextPageToken, resultSizeEstimate, receivedAtMs, rawJson) VALUES(?, ?, ?, ?, ?)`,
			requestID, next, r.ResultSizeEstimate, now, string(rawResp))
		if err != nil {
			tx.Rollback()
			return err
		}
		responseID, err := res.LastInsertId()
		if err != nil {
			tx.Rollback()
			return err
		}

		for _, m := range r.Messages {
			rawMsg, _ := json.Marshal(m)
			if _, err := tx.Exec(`INSERT INTO gmail_users_messages_list_response_messages(responseId, id, threadId, rawJson) VALUES(?, ?, ?, ?)`, responseID, m.Id, m.ThreadId, string(rawMsg)); err != nil {
				tx.Rollback()
				return err
			}
			if _, err := tx.Exec(`INSERT INTO gmail_users_messages_index(id, threadId, lastResponseId, updatedAtMs, rawJson)
				VALUES(?, ?, ?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET threadId=excluded.threadId, lastResponseId=excluded.lastResponseId, updatedAtMs=excluded.updatedAtMs, rawJson=excluded.rawJson`,
				m.Id, m.ThreadId, responseID, now, string(rawMsg)); err != nil {
				tx.Rollback()
				return err
			}
		}

		if next == "" {
			if err := setSyncState(tx, syncStateListDone, "1"); err != nil {
				tx.Rollback()
				return err
			}
			if err := setSyncState(tx, syncStateListNextPageToken, ""); err != nil {
				tx.Rollback()
				return err
			}
		} else {
			if err := setSyncState(tx, syncStateListDone, "0"); err != nil {
				tx.Rollback()
				return err
			}
			if err := setSyncState(tx, syncStateListNextPageToken, next); err != nil {
				tx.Rollback()
				return err
			}
		}

		if err := tx.Commit(); err != nil {
			return err
		}

		pages++
		msgs += len(r.Messages)
		log.Printf("listing: page=%d messages=%d total_messages=%d nextPageToken=%t", pages, len(r.Messages), msgs, next != "")

		if next == "" {
			break
		}
		pageToken = next
	}
	log.Printf("listing: complete pages=%d total_messages=%d", pages, msgs)
	return nil
}

func persistOAuthToken(db *sql.DB, account string, tok *oauth2.Token) error {
	raw, _ := json.Marshal(tok)
	exp := int64(0)
	if !tok.Expiry.IsZero() {
		exp = tok.Expiry.UnixMilli()
	}
	now := time.Now().UnixMilli()
	_, err := db.Exec(`INSERT INTO oauth_tokens(account, tokenType, accessToken, refreshToken, expiryUnixMs, scope, rawJson, updatedAtMs)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account) DO UPDATE SET tokenType=excluded.tokenType, accessToken=excluded.accessToken, refreshToken=excluded.refreshToken, expiryUnixMs=excluded.expiryUnixMs, scope=excluded.scope, rawJson=excluded.rawJson, updatedAtMs=excluded.updatedAtMs`,
		account, tok.TokenType, tok.AccessToken, tok.RefreshToken, exp, "", string(raw), now)
	return err
}

