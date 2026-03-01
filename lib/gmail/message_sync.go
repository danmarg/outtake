package gmail

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/danmarg/outtake/lib/maildir"
	_ "modernc.org/sqlite"
)

type listedMessage struct {
	ResponseID int64
	MessageID  string
}

type phase2WorkItem struct {
	Seq int64
	Msg listedMessage
}

type phase2Result struct {
	Seq        int64
	Msg        listedMessage
	Downloaded bool
	Skipped    bool
	Failed     bool
	Err        error
}

func (g *Gmail) SyncListedMessagesWithDB(db *sql.DB) error {
	if err := ensureListPagesSchema(db); err != nil {
		return err
	}

	lastRespID, lastMsgID, err := getMaterializeCheckpoint(db)
	if err != nil {
		return err
	}
	if lastRespID > 0 {
		log.Printf("downloading-archived: resume from responseId=%d messageId=%q", lastRespID, lastMsgID)
	} else {
		log.Printf("downloading-archived: start from newest listed message")
	}

	total, err := ensureMaterializeTotalMessages(db)
	if err != nil {
		return err
	}
	remaining, err := countRemainingListedMessages(db, lastRespID, lastMsgID)
	if err != nil {
		return err
	}
	alreadyDone := total - remaining
	if alreadyDone < 0 {
		alreadyDone = 0
	}
	log.Printf("downloading-archived: queued=%d total=%d done=%d workers=%d", remaining, total, alreadyDone, ConcurrentDownloads)

	start := time.Now()
	lastPerfLog := time.Now()
	lastCheckpointFlush := time.Now()
	checkpointFlushEvery := 50
	checkpointFlushInterval := 2 * time.Second

	workCh := make(chan phase2WorkItem, MessageBufferSize)
	resultCh := make(chan phase2Result, MessageBufferSize)

	// Producer
	prodErrCh := make(chan error, 1)
	go func() {
		defer close(workCh)
		seq := int64(1)
		cursorResp, cursorMsg := lastRespID, lastMsgID
		for {
			batch, err := nextListedMessagesBatch(db, cursorResp, cursorMsg, 500)
			if err != nil {
				prodErrCh <- err
				return
			}
			if len(batch) == 0 {
				prodErrCh <- nil
				return
			}
			for _, m := range batch {
				workCh <- phase2WorkItem{Seq: seq, Msg: m}
				seq++
				cursorResp, cursorMsg = m.ResponseID, m.MessageID
			}
		}
	}()

	labelsRefreshedOnMiss := false
	var labelsRefreshMu sync.Mutex

	// Workers
	workers := ConcurrentDownloads
	if workers < 1 {
		workers = 1
	}
	wg := sync.WaitGroup{}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range workCh {
				res := phase2Result{Seq: item.Seq, Msg: item.Msg}
				currentI := alreadyDone + int(item.Seq)
				downloadedNow, skippedNow, err := g.downloadAndWriteListedMessage(db, item.Msg.MessageID, total, currentI, &labelsRefreshedOnMiss, &labelsRefreshMu)
				if err != nil {
					res.Failed = true
					res.Err = err
					resultCh <- res
					continue
				}
				res.Downloaded = downloadedNow
				res.Skipped = skippedNow
				resultCh <- res
			}
		}()
	}
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var downloaded, skipped, failed int
	skippedNoticeLogged := false
	nextToApply := int64(1)
	pending := map[int64]phase2Result{}
	var latestCheckpoint listedMessage
	haveCheckpoint := false
	unflushedApplied := 0

	flushCheckpoint := func(force bool) error {
		if !haveCheckpoint {
			return nil
		}
		if !force && unflushedApplied < checkpointFlushEvery && time.Since(lastCheckpointFlush) < checkpointFlushInterval {
			return nil
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if err := setMaterializeCheckpoint(tx, latestCheckpoint.ResponseID, latestCheckpoint.MessageID); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		unflushedApplied = 0
		lastCheckpointFlush = time.Now()
		return nil
	}

	for r := range resultCh {
		pending[r.Seq] = r

		for {
			curr, ok := pending[nextToApply]
			if !ok {
				break
			}
			delete(pending, nextToApply)
			nextToApply++

			if curr.Downloaded {
				downloaded++
			} else if curr.Skipped {
				skipped++
			} else if curr.Failed {
				failed++
				log.Printf("downloading-archived: message=%s failed: %v", curr.Msg.MessageID, curr.Err)
			}

			latestCheckpoint = curr.Msg
			haveCheckpoint = true
			unflushedApplied++
		}

		if err := flushCheckpoint(false); err != nil {
			return err
		}

		if time.Since(lastPerfLog) >= 2*time.Second {
			elapsed := time.Since(start).Seconds()
			if elapsed <= 0 {
				elapsed = 0.001
			}
			processedRun := downloaded + skipped + failed
			processed := alreadyDone + processedRun
			pct := 0.0
			if total > 0 {
				pct = float64(processed) / float64(total) * 100.0
			}
			rateDownloaded := float64(downloaded) / elapsed
			secPerMsg := elapsed / float64(maxInt(downloaded, 1))
			remainingItems := maxInt(total-processed, 0)
			etaSec := 0.0
			if rateDownloaded > 0 {
				etaSec = float64(remainingItems) / rateDownloaded
			}
			log.Printf("downloading-archived: perf progress=%d/%d %.2f%% eta=%s downloaded=%d failed=%d rate_downloaded=%.2f msg/s latency=%.3f s/msg",
				processed, total, pct, etaString(etaSec), downloaded, failed, rateDownloaded, secPerMsg)
			if skipped > 0 && !skippedNoticeLogged {
				log.Printf("downloading-archived: note skipped=%d (already present from replay after resume)", skipped)
				skippedNoticeLogged = true
			}
			lastPerfLog = time.Now()
		}
	}

	if err := flushCheckpoint(true); err != nil {
		return err
	}
	if err := <-prodErrCh; err != nil {
		return err
	}

	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		elapsed = 0.001
	}
	if skipped > 0 && !skippedNoticeLogged {
		log.Printf("downloading-archived: note skipped=%d (already present from replay after resume)", skipped)
	}
	log.Printf("downloading-archived: complete downloaded=%d failed=%d elapsed=%.1fs rate_downloaded=%.2f msg/s",
		downloaded, failed, elapsed, float64(downloaded)/elapsed)
	return nil
}

func ensureMaterializeTotalMessages(db *sql.DB) (int, error) {
	if v, ok, err := getSyncState(db, syncStateMaterializeTotalMessages); err != nil {
		return 0, err
	} else if ok {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n, nil
		}
	}

	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gmail_users_messages_list_response_messages`).Scan(&total); err != nil {
		return 0, err
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	if err := setSyncState(tx, syncStateMaterializeTotalMessages, strconv.Itoa(total)); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return total, nil
}

func countRemainingListedMessages(db *sql.DB, lastRespID int64, lastMsgID string) (int, error) {
	q := `SELECT COUNT(*) FROM gmail_users_messages_list_response_messages`
	args := []interface{}{}
	if lastRespID > 0 {
		q += ` WHERE (responseId > ?) OR (responseId = ? AND id > ?)`
		args = append(args, lastRespID, lastRespID, lastMsgID)
	}
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func nextListedMessagesBatch(db *sql.DB, lastRespID int64, lastMsgID string, limit int) ([]listedMessage, error) {
	base := `SELECT responseId, id
		FROM gmail_users_messages_list_response_messages`
	args := []interface{}{}
	if lastRespID > 0 {
		base += ` WHERE (responseId > ?) OR (responseId = ? AND id > ?)`
		args = append(args, lastRespID, lastRespID, lastMsgID)
	}
	base += ` ORDER BY responseId ASC, id ASC LIMIT ?`
	args = append(args, limit)
	return queryListedMessages(db, base, args, limit)
}

func firstListedMessage(db *sql.DB) (listedMessage, bool, error) {
	base := `SELECT responseId, id
		FROM gmail_users_messages_list_response_messages
		ORDER BY responseId ASC, id ASC LIMIT 1`
	rows, err := queryListedMessages(db, base, nil, 1)
	if err != nil {
		return listedMessage{}, false, err
	}
	if len(rows) == 0 {
		return listedMessage{}, false, nil
	}
	return rows[0], true, nil
}

func queryListedMessages(db *sql.DB, query string, args []interface{}, capHint int) ([]listedMessage, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if capHint < 1 {
		capHint = 1
	}
	out := make([]listedMessage, 0, capHint)
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

func (g *Gmail) downloadAndWriteListedMessage(db *sql.DB, id string, total, currentI int, labelsRefreshedOnMiss *bool, labelsRefreshMu *sync.Mutex) (bool, bool, error) {
	_ = total
	_ = currentI
	stableKey := messageMaildirKey(id)
	if _, err := g.dir.GetFile(stableKey); err == nil {
		return false, true, nil
	}

	op := g.handleNewMsg(id)
	if op.Error != nil {
		return false, false, op.Error
	}
	if op.Operation == NONE {
		return false, true, nil
	}
	if op.Operation != ADD {
		return false, false, fmt.Errorf("unexpected operation for listed message %s: %d", id, op.Operation)
	}

	mappedLabels, err := g.resolveArchivedLabels(db, op.Labels, labelsRefreshedOnMiss, labelsRefreshMu)
	if err != nil {
		return false, false, err
	}
	op.Msg.Header[labelsHeader] = mappedLabels

	if _, err := g.dir.DeliverWithKey(op.Msg, stableKey); err != nil {
		return false, false, err
	}
	if err := replaceMessageLabels(db, id, op.Labels); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func (g *Gmail) resolveArchivedLabels(db *sql.DB, ids []string, labelsRefreshedOnMiss *bool, labelsRefreshMu *sync.Mutex) ([]string, error) {
	mapped, hadUnknown, err := resolveLabelNames(db, ids)
	if err != nil {
		return nil, err
	}
	if !hadUnknown {
		return mapped, nil
	}

	labelsRefreshMu.Lock()
	defer labelsRefreshMu.Unlock()

	mapped, hadUnknown, err = resolveLabelNames(db, ids)
	if err != nil {
		return nil, err
	}
	if !hadUnknown {
		return mapped, nil
	}

	if !*labelsRefreshedOnMiss {
		if err := g.refreshLabelsInDB(db); err != nil {
			return nil, err
		}
		*labelsRefreshedOnMiss = true
	}
	mapped, _, err = resolveLabelNames(db, ids)
	if err != nil {
		return nil, err
	}
	return mapped, nil
}

func (g *Gmail) refreshLabelsInDB(db *sql.DB) error {
	resp, err := g.svc.GetLabels()
	if err != nil {
		return err
	}
	refreshed, err := upsertLabels(db, resp.Labels)
	if err != nil {
		return err
	}
	log.Printf("downloading-archived: labels refreshed=%d", refreshed)
	return nil
}

func stableArchiveKey(total, currentI int, id string) maildir.Key {
	return messageMaildirKey(id)
}

func messageMaildirKey(id string) maildir.Key {
	return maildir.Key(fmt.Sprintf("%s.mail", id))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func etaString(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	d := time.Duration(seconds * float64(time.Second)).Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
