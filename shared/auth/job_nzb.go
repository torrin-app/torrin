package auth

import "context"

func (s *Store) SetJobNZB(ctx context.Context, infoHash string, nzb []byte) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO job_nzbs (info_hash, nzb) VALUES ($1,$2)
		 ON CONFLICT (info_hash) DO UPDATE SET nzb = $2`,
		infoHash, nzb)
	return err
}

func (s *Store) GetJobNZB(ctx context.Context, infoHash string) ([]byte, bool) {
	var nzb []byte
	err := s.pool.QueryRow(ctx, `SELECT nzb FROM job_nzbs WHERE info_hash = $1`, infoHash).Scan(&nzb)
	return nzb, err == nil && len(nzb) > 0
}

func (s *Store) ClearJobNZB(ctx context.Context, infoHash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM job_nzbs WHERE info_hash = $1`, infoHash)
	return err
}
