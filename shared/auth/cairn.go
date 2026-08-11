package auth

import (
	"context"
	"time"
)

type CairnItem struct {
	InfoHash  string    `json:"info_hash"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	Archived  bool      `json:"archived"`
}

func (s *Store) SetCairnArchive(ctx context.Context, infoHash, nzbKey, name string, size int64, nzb []byte) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO cairn_archives (info_hash, nzb_key, name, size, nzb) VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (info_hash) DO UPDATE SET nzb_key = $2, name = $3, size = $4, nzb = $5`,
		infoHash, nzbKey, name, size, nzb)
	return err
}

func (s *Store) GetCairnArchive(ctx context.Context, infoHash string) (nzbKey, name string, ok bool) {
	err := s.pool.QueryRow(ctx, `SELECT nzb_key, name FROM cairn_archives WHERE info_hash = $1`, infoHash).Scan(&nzbKey, &name)
	return nzbKey, name, err == nil && nzbKey != ""
}

func (s *Store) GetCairnNZB(ctx context.Context, infoHash string) ([]byte, bool) {
	var nzb []byte
	err := s.pool.QueryRow(ctx, `SELECT nzb FROM cairn_archives WHERE info_hash = $1`, infoHash).Scan(&nzb)
	return nzb, err == nil && len(nzb) > 0
}

type CairnNZB struct {
	InfoHash string
	NZB      []byte
}

func (s *Store) CairnArchivesToBackfill(ctx context.Context, limit int) ([]CairnNZB, error) {
	rows, err := s.pool.Query(ctx, `SELECT info_hash, nzb FROM cairn_archives WHERE nzb IS NOT NULL LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CairnNZB
	for rows.Next() {
		var c CairnNZB
		if err := rows.Scan(&c.InfoHash, &c.NZB); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ClearCairnNZB(ctx context.Context, infoHash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE cairn_archives SET nzb = NULL WHERE info_hash = $1`, infoHash)
	return err
}

func (s *Store) PendingCairns(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT c.info_hash FROM user_cairns c
		LEFT JOIN cairn_archives a ON a.info_hash = c.info_hash
		WHERE a.info_hash IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) AddUserCairn(ctx context.Context, userID, infoHash string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_cairns (user_id, info_hash) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		userID, infoHash)
	return err
}

func (s *Store) DeleteUserCairn(ctx context.Context, userID, infoHash string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM user_cairns WHERE user_id = $1 AND info_hash = $2`, userID, infoHash)
	return err
}

func (s *Store) ListUserCairns(ctx context.Context, userID string) ([]CairnItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.info_hash, COALESCE(a.name, ''), COALESCE(a.size, 0), c.created_at, a.info_hash IS NOT NULL
		FROM user_cairns c
		LEFT JOIN cairn_archives a ON a.info_hash = c.info_hash
		WHERE c.user_id = $1
		ORDER BY c.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CairnItem{}
	for rows.Next() {
		var it CairnItem
		if err := rows.Scan(&it.InfoHash, &it.Name, &it.Size, &it.CreatedAt, &it.Archived); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
