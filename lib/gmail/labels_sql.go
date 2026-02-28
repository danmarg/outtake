package gmail

import (
	"database/sql"
	"time"

	gmailapi "google.golang.org/api/gmail/v1"
)

func upsertLabels(db *sql.DB, labels []*gmailapi.Label) (int, error) {
	if len(labels) == 0 {
		return 0, nil
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	for _, l := range labels {
		if l == nil || l.Id == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO gmail_labels(id, name, type, updatedAtMs)
			VALUES(?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET name=excluded.name, type=excluded.type, updatedAtMs=excluded.updatedAtMs`,
			l.Id, l.Name, l.Type, now); err != nil {
			tx.Rollback()
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(labels), nil
}

func resolveLabelName(db *sql.DB, id string) (string, bool, error) {
	var name string
	err := db.QueryRow(`SELECT name FROM gmail_labels WHERE id = ?`, id).Scan(&name)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return name, true, nil
}

func resolveLabelNames(db *sql.DB, ids []string) (mapped []string, hadUnknown bool, err error) {
	mapped = make([]string, 0, len(ids))
	for _, id := range ids {
		name, ok, err := resolveLabelName(db, id)
		if err != nil {
			return nil, false, err
		}
		if ok {
			mapped = append(mapped, name)
		} else {
			hadUnknown = true
			mapped = append(mapped, id)
		}
	}
	return mapped, hadUnknown, nil
}
