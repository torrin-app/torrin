package jobs

import "context"

type Blob struct {
	ContentKey string
	Size       int64
	Encrypted  bool
}

func (p *Postgres) LookupBlob(ctx context.Context, contentKey string) (*Blob, error) {
	b := &Blob{ContentKey: contentKey}
	err := p.pool.QueryRow(ctx,
		`SELECT size, encrypted FROM blobs WHERE content_key=$1`, contentKey).Scan(&b.Size, &b.Encrypted)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (p *Postgres) AddBlobRef(ctx context.Context, infoHash string, idx int, contentKey string, size int64, encrypted bool) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`INSERT INTO blobs (content_key, size, encrypted) VALUES ($1,$2,$3) ON CONFLICT (content_key) DO NOTHING`,
		contentKey, size, encrypted); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO blob_refs (info_hash, file_index, content_key) VALUES ($1,$2,$3)
		 ON CONFLICT (info_hash, file_index) DO UPDATE SET content_key=EXCLUDED.content_key`,
		infoHash, idx, contentKey); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) DropBlobRefs(ctx context.Context, infoHash string) ([]string, error) {
	rows, err := p.pool.Query(ctx,
		`WITH removed AS (
			DELETE FROM blob_refs WHERE info_hash=$1 RETURNING content_key
		)
		SELECT DISTINCT r.content_key FROM removed r
		WHERE NOT EXISTS (SELECT 1 FROM blob_refs b WHERE b.content_key=r.content_key)`,
		infoHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var orphaned []string
	for rows.Next() {
		var ck string
		if rows.Scan(&ck) == nil {
			orphaned = append(orphaned, ck)
		}
	}
	return orphaned, rows.Err()
}

func (p *Postgres) DeleteBlob(ctx context.Context, contentKey string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM blobs WHERE content_key=$1`, contentKey)
	return err
}

func (p *Postgres) OrphanedBlobs(ctx context.Context, limit int) ([]Blob, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT content_key, size FROM blobs b
		 WHERE NOT EXISTS (SELECT 1 FROM blob_refs r WHERE r.content_key = b.content_key)
		 LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Blob
	for rows.Next() {
		var b Blob
		if err := rows.Scan(&b.ContentKey, &b.Size); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
