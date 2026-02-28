package gmail

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

type listedMessage struct {
	ResponseID int64
	MessageID  string
}

func (g *Gmail) SyncListedMessages(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := ensureListPagesSchema(db); err != nil {
		return err
	}

	lastRespID, lastMsgID, err := getMaterializeCheckpoint(db)
	if err != nil {
		return err
	}
	if lastRespID > 0 {
		log.Printf("phase2: resume from responseId=%d messageId=%q", lastRespID, lastMsgID)
	} else {
		log.Printf("phase2: start from newest listed message")
	}

	var downloaded, skipped, failed int
	start := time.Now()
	lastPerfLog := time.Now()

	for {
		batch, err := nextListedMessagesBatch(db, lastRespID, lastMsgID, 200)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			break
		}

		for _, item := range batch {
			if _, exists := g.cache.GetMsgKey(item.MessageID); exists {
				skipped++
			} else {
				if err := g.downloadAndWriteListedMessage(item.MessageID); err != nil {
					failed++
					log.Printf("phase2: message=%s failed: %v", item.MessageID, err)
					continue
				}
				downloaded++
			}

			tx, err := db.Begin()
			if err != nil {
				return err
			}
			if err := setMaterializeCheckpoint(tx, item.ResponseID, item.MessageID); err != nil {
				tx.Rollback()
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}

			lastRespID = item.ResponseID
			lastMsgID = item.MessageID

			if time.Since(lastPerfLog) >= 2*time.Second {
				elapsed := time.Since(start).Seconds()
				if elapsed <= 0 {
					elapsed = 0.001
				}
				totalDone := downloaded + skipped + failed
				msgPerSec := float64(totalDone) / elapsed
				secPerMsg := elapsed / float64(maxInt(totalDone, 1))
				log.Printf("phase2: perf done=%d downloaded=%d skipped=%d failed=%d rate=%.2f msg/s latency=%.3f s/msg",
					totalDone, downloaded, skipped, failed, msgPerSec, secPerMsg)
				lastPerfLog = time.Now()
			}
		}
	}

	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		elapsed = 0.001
	}
	totalDone := downloaded + skipped + failed
	log.Printf("phase2: complete downloaded=%d skipped=%d failed=%d elapsed=%.1fs rate=%.2f msg/s",
		downloaded, skipped, failed, elapsed, float64(totalDone)/elapsed)
	return nil
}

func nextListedMessagesBatch(db *sql.DB, lastRespID int64, lastMsgID string, limit int) ([]listedMessage, error) {
	base := `SELECT responseId, id
		FROM gmail_users_messages_list_response_messages`
	args := []interface{}{}
	if lastRespID > 0 {
		base += ` WHERE (responseId < ?) OR (responseId = ? AND id < ?)`
		args = append(args, lastRespID, lastRespID, lastMsgID)
	}
	base += ` ORDER BY responseId DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(base, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]listedMessage, 0, limit)
	for rows.Next() {
		var m listedMessage
		if err := rows.Scan(&m.ResponseID, &m.MessageID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (g *Gmail) downloadAndWriteListedMessage(id string) error {
	op := g.handleNewMsg(id)
	if op.Error != nil {
		return op.Error
	}
	if op.Operation == NONE {
		return nil
	}
	if op.Operation != ADD {
		return fmt.Errorf("unexpected operation for listed message %s: %d", id, op.Operation)
	}
	return g.writeOperation(op)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
