package jobs

import "context"

func (p *Postgres) MapNzbURL(ctx context.Context, urlHash, contentHash string) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO nzb_url_hashes (url_hash, content_hash) VALUES ($1,$2)
		 ON CONFLICT (url_hash) DO UPDATE SET content_hash=$2`,
		urlHash, contentHash)
	return err
}

func (p *Postgres) ContentHashByURL(ctx context.Context, urlHash string) (string, error) {
	var ch string
	err := p.pool.QueryRow(ctx,
		`SELECT content_hash FROM nzb_url_hashes WHERE url_hash=$1`, urlHash).Scan(&ch)
	return ch, err
}

func (p *Postgres) ContentHashesByURL(ctx context.Context, urlHashes []string) (map[string]string, error) {
	out := map[string]string{}
	rows, err := p.pool.Query(ctx,
		`SELECT url_hash, content_hash FROM nzb_url_hashes WHERE url_hash = ANY($1)`, urlHashes)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var u, c string
		if err := rows.Scan(&u, &c); err != nil {
			return out, err
		}
		out[u] = c
	}
	return out, rows.Err()
}

func (p *Postgres) URLHashesByContent(ctx context.Context, contentHashes []string) (map[string]string, error) {
	out := map[string]string{}
	rows, err := p.pool.Query(ctx,
		`SELECT content_hash, url_hash FROM nzb_url_hashes WHERE content_hash = ANY($1)`, contentHashes)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var c, u string
		if err := rows.Scan(&c, &u); err != nil {
			return out, err
		}
		if _, ok := out[c]; !ok {
			out[c] = u
		}
	}
	return out, rows.Err()
}
