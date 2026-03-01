package gmail

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"google.golang.org/api/googleapi"
)

func (g *Gmail) SyncHistoryWithDB(db *sql.DB) error {
	if err := ensureListPagesSchema(db); err != nil {
		return err
	}

	cursor, pageToken, err := getHistoryResumeState(db)
	if err != nil {
		return err
	}
	if cursor == 0 {
		cursor, err = g.bootstrapHistoryCursor(db)
		if err != nil {
			return err
		}
		if cursor == 0 {
			log.Printf("history: skipped (no bootstrap history cursor available)")
			return nil
		}
		if err := setHistoryProgress(db, cursor, ""); err != nil {
			return err
		}
	}

	log.Printf("history: start cursor=%d resume_page=%t", cursor, pageToken != "")
	start := time.Now()
	lastPerf := time.Now()
	maxSeen := cursor
	var added, deleted, labeled, events int
	var labelsMissingFile, labelsMissingState, labelsFailed int
	labelsRefreshedOnMiss := false
	var labelsRefreshMu sync.Mutex

	for {
		r, err := g.svc.GetHistory(cursor, g.labelId, pageToken)
		if err != nil {
			if e, ok := err.(*googleapi.Error); ok && e.Code == 404 {
				log.Printf("history: cursor expired, clearing state and falling back to Listing + Downloading-Archived")
				if err := clearHistoryState(db); err != nil {
					return err
				}
				if err := g.SyncListPagesWithDB(db); err != nil {
					return err
				}
				return g.SyncListedMessagesWithDB(db)
			}
			return err
		}

		for _, h := range r.History {
			events++
			if h.Id > maxSeen {
				maxSeen = h.Id
			}
			for _, a := range h.MessagesAdded {
				if a.Message == nil || a.Message.Id == "" {
					continue
				}
				didAdd, err := g.downloadAndWriteHistoryMessage(db, a.Message.Id, &labelsRefreshedOnMiss, &labelsRefreshMu)
				if err != nil {
					return err
				}
				if didAdd {
					added++
				}
			}
			for _, d := range h.MessagesDeleted {
				if d.Message == nil || d.Message.Id == "" {
					continue
				}
				if err := g.deleteMessageByID(d.Message.Id); err == nil {
					deleted++
				}
			}
			for _, l := range h.LabelsAdded {
				if l.Message == nil || l.Message.Id == "" {
					continue
				}
				applied, missingFile, missingState, err := g.applyHistoryLabelDelta(db, l.Message.Id, l.LabelIds, nil, &labelsRefreshedOnMiss, &labelsRefreshMu)
				if err != nil {
					labelsFailed++
					continue
				}
				if applied {
					labeled++
				}
				if missingFile {
					labelsMissingFile++
				}
				if missingState {
					labelsMissingState++
				}
			}
			for _, l := range h.LabelsRemoved {
				if l.Message == nil || l.Message.Id == "" {
					continue
				}
				applied, missingFile, missingState, err := g.applyHistoryLabelDelta(db, l.Message.Id, nil, l.LabelIds, &labelsRefreshedOnMiss, &labelsRefreshMu)
				if err != nil {
					labelsFailed++
					continue
				}
				if applied {
					labeled++
				}
				if missingFile {
					labelsMissingFile++
				}
				if missingState {
					labelsMissingState++
				}
			}
		}

		pageToken = r.NextPageToken
		if err := setHistoryProgress(db, maxSeen, pageToken); err != nil {
			return err
		}

		if time.Since(lastPerf) > 2*time.Second {
			elapsed := time.Since(start).Seconds()
			if elapsed <= 0 {
				elapsed = 0.001
			}
			log.Printf("history: perf events=%d added=%d deleted=%d labels_applied=%d labels_missing_file=%d labels_missing_state=%d labels_failed=%d rate=%.2f ev/s cursor=%d page=%t elapsed=%.1fs",
				events, added, deleted, labeled, labelsMissingFile, labelsMissingState, labelsFailed, float64(events)/elapsed, maxSeen, pageToken != "", elapsed)
			lastPerf = time.Now()
		}

		if pageToken == "" {
			break
		}
	}

	if err := commitHistoryCursor(db, maxSeen); err != nil {
		return err
	}
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		elapsed = 0.001
	}
	log.Printf("history: complete cursor=%d events=%d added=%d deleted=%d labels_applied=%d labels_missing_file=%d labels_missing_state=%d labels_failed=%d elapsed=%.1fs rate=%.2f ev/s",
		maxSeen, events, added, deleted, labeled, labelsMissingFile, labelsMissingState, labelsFailed, elapsed, float64(events)/elapsed)
	return nil
}

func getHistoryResumeState(db *sql.DB) (uint64, string, error) {
	if v, ok, err := getSyncState(db, syncStateHistoryCursorProgress); err != nil {
		return 0, "", err
	} else if ok && v != "" {
		u, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, "", err
		}
		p, _, err := getSyncState(db, syncStateHistoryPageToken)
		return u, p, err
	}
	if v, ok, err := getSyncState(db, syncStateHistoryCursorCommitted); err != nil {
		return 0, "", err
	} else if ok && v != "" {
		u, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, "", err
		}
		return u, "", nil
	}
	return 0, "", nil
}

func setHistoryProgress(db *sql.DB, cursor uint64, pageToken string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := setSyncState(tx, syncStateHistoryCursorProgress, strconv.FormatUint(cursor, 10)); err != nil {
		tx.Rollback()
		return err
	}
	if err := setSyncState(tx, syncStateHistoryPageToken, pageToken); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func commitHistoryCursor(db *sql.DB, cursor uint64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := setSyncState(tx, syncStateHistoryCursorCommitted, strconv.FormatUint(cursor, 10)); err != nil {
		tx.Rollback()
		return err
	}
	for _, k := range []string{syncStateHistoryCursorProgress, syncStateHistoryPageToken} {
		if _, err := tx.Exec(`DELETE FROM sync_state WHERE key = ?`, k); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func clearHistoryState(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, k := range []string{syncStateHistoryCursorCommitted, syncStateHistoryCursorProgress, syncStateHistoryPageToken} {
		if _, err := tx.Exec(`DELETE FROM sync_state WHERE key = ?`, k); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (g *Gmail) bootstrapHistoryCursor(db *sql.DB) (uint64, error) {
	msg, ok, err := firstListedMessage(db)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	m, err := g.svc.GetMetadata(msg.MessageID)
	if err != nil {
		return 0, fmt.Errorf("history bootstrap failed: cannot fetch metadata for first listed message id=%s (responseId=%d): %w", msg.MessageID, msg.ResponseID, err)
	}
	if m == nil || m.HistoryId == 0 {
		return 0, fmt.Errorf("history bootstrap failed: first listed message id=%s (responseId=%d) has no usable historyId", msg.MessageID, msg.ResponseID)
	}
	log.Printf("history: bootstrapped cursor=%d from first listed message=%s responseId=%d", m.HistoryId, msg.MessageID, msg.ResponseID)
	return m.HistoryId, nil
}

func (g *Gmail) downloadAndWriteHistoryMessage(db *sql.DB, id string, labelsRefreshedOnMiss *bool, labelsRefreshMu *sync.Mutex) (bool, error) {
	k := messageMaildirKey(id)
	if _, err := g.dir.GetFile(k); err == nil {
		return false, nil
	}
	op := g.handleNewMsg(id)
	if op.Error != nil {
		return false, op.Error
	}
	if op.Operation == NONE {
		return false, nil
	}
	if op.Operation != ADD {
		return false, nil
	}
	mapped, err := g.resolveArchivedLabels(db, op.Labels, labelsRefreshedOnMiss, labelsRefreshMu)
	if err != nil {
		return false, err
	}
	op.Msg.Header[labelsHeader] = mapped
	if _, err := g.dir.DeliverWithKey(op.Msg, k); err != nil {
		return false, err
	}
	if err := replaceMessageLabels(db, id, op.Labels); err != nil {
		return false, err
	}
	return true, nil
}

func (g *Gmail) deleteMessageByID(id string) error {
	k := messageMaildirKey(id)
	if err := g.dir.Delete(k); err == nil {
		return nil
	}
	return g.writeDel(id)
}

func (g *Gmail) applyHistoryLabelDelta(db *sql.DB, id string, add, remove []string, labelsRefreshedOnMiss *bool, labelsRefreshMu *sync.Mutex) (applied bool, missingFile bool, missingState bool, err error) {
	labels, err := getMessageLabels(db, id)
	if err != nil {
		return false, false, false, err
	}
	if len(labels) == 0 {
		return false, false, true, nil
	}
	if err := applyLabelDelta(db, id, add, remove); err != nil {
		return false, false, false, err
	}
	labels, err = getMessageLabels(db, id)
	if err != nil {
		return false, false, false, err
	}
	mapped, err := g.resolveArchivedLabels(db, labels, labelsRefreshedOnMiss, labelsRefreshMu)
	if err != nil {
		return false, false, false, err
	}
	k := messageMaildirKey(id)
	msg, c, err := g.getMaildirMessage(k)
	if err != nil {
		return false, true, false, nil
	}
	defer c.Close()
	msg.Header[labelsHeader] = mapped
	if err := g.dir.Delete(k); err != nil {
		return false, false, false, err
	}
	if _, err := g.dir.DeliverWithKey(msg, k); err != nil {
		return false, false, false, err
	}
	return true, false, false, nil
}
