package auth

import (
	"context"
)

type LibraryEntry struct {
	InfoHash string
	Filename string
	Filesize int64
}

func (s *Store) GetAllRDKeys(ctx context.Context) (map[string]string, error) {
	return s.allKeys(ctx, "rd_credentials")
}

func (s *Store) GetAllTBKeys(ctx context.Context) (map[string]string, error) {
	return s.allKeys(ctx, "tb_credentials")
}

func (s *Store) allKeys(ctx context.Context, table string) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT user_id, api_key FROM `+table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make(map[string]string)
	for rows.Next() {
		var uid, key string
		if rows.Scan(&uid, &key) == nil {
			keys[uid] = s.dec(key)
		}
	}
	return keys, rows.Err()
}

func (s *Store) SyncRDLibrary(ctx context.Context, userID string, entries []LibraryEntry) error {
	return s.syncLibrary(ctx, "rd_library", userID, entries)
}

func (s *Store) SyncTBLibrary(ctx context.Context, userID string, entries []LibraryEntry) error {
	return s.syncLibrary(ctx, "tb_library", userID, entries)
}

func (s *Store) syncLibrary(ctx context.Context, table, userID string, entries []LibraryEntry) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE user_id=$1`, userID); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := tx.Exec(ctx,
			`INSERT INTO `+table+` (info_hash, user_id, filename, filesize) VALUES ($1,$2,$3,$4)
			 ON CONFLICT (info_hash, user_id) DO NOTHING`,
			e.InfoHash, userID, e.Filename, e.Filesize); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) SyncADLibrary(ctx context.Context, entries []LibraryEntry) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM ad_library`); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := tx.Exec(ctx,
			`INSERT INTO ad_library (info_hash, filename, filesize) VALUES ($1,$2,$3)
			 ON CONFLICT (info_hash) DO NOTHING`, e.InfoHash, e.Filename, e.Filesize); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) IsInADLibrary(ctx context.Context, infoHash string) bool {
	var n int
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ad_library WHERE info_hash=$1`, infoHash).Scan(&n)
	return n > 0
}
