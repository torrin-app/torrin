package auth

import (
	"context"
	"strconv"
	"time"
)

type WebdavOverride struct {
	InfoHash  string `json:"info_hash"`
	FileIndex int    `json:"file_index"`
	Alias     string `json:"alias"`
	Excluded  bool   `json:"excluded"`
}

func WebdavKey(infoHash string, fileIndex int) string {
	return infoHash + ":" + strconv.Itoa(fileIndex)
}

func (s *Store) WebdavOverrides(ctx context.Context, userID string) (map[string]WebdavOverride, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT info_hash, file_index, alias, excluded FROM webdav_overrides WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]WebdavOverride{}
	for rows.Next() {
		var o WebdavOverride
		if rows.Scan(&o.InfoHash, &o.FileIndex, &o.Alias, &o.Excluded) == nil {
			out[WebdavKey(o.InfoHash, o.FileIndex)] = o
		}
	}
	return out, rows.Err()
}

func (s *Store) ListWebdavOverrides(ctx context.Context, userID string) ([]WebdavOverride, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT info_hash, file_index, alias, excluded FROM webdav_overrides WHERE user_id=$1 ORDER BY info_hash, file_index`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WebdavOverride{}
	for rows.Next() {
		var o WebdavOverride
		if rows.Scan(&o.InfoHash, &o.FileIndex, &o.Alias, &o.Excluded) == nil {
			out = append(out, o)
		}
	}
	return out, rows.Err()
}

func (s *Store) SetWebdavOverride(ctx context.Context, userID, infoHash string, fileIndex int, alias string, excluded bool) error {
	if alias == "" && !excluded {
		_, err := s.pool.Exec(ctx,
			`DELETE FROM webdav_overrides WHERE user_id=$1 AND info_hash=$2 AND file_index=$3`,
			userID, infoHash, fileIndex)
		return err
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO webdav_overrides (user_id, info_hash, file_index, alias, excluded, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (user_id, info_hash, file_index)
		 DO UPDATE SET alias=$4, excluded=$5, updated_at=$6`,
		userID, infoHash, fileIndex, alias, excluded, time.Now())
	return err
}
