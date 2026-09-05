package jobs

import (
	"context"
	"encoding/json"
)

// BYOSByHashes only exposes completed copies belonging to the requesting user
// whose private storage is still enabled and supported by the streaming route.
func (p *Postgres) BYOSByHashes(ctx context.Context, userID string, hashes []string) (map[string]*BYOSObject, error) {
	rows, err := p.pool.Query(ctx, `SELECT DISTINCT ON (o.info_hash) o.user_id,o.bucket,o.info_hash,o.name,o.streams_json
 FROM byos_objects o JOIN storage_credentials c ON c.user_id=o.user_id
 WHERE o.user_id=$1 AND o.info_hash=ANY($2) AND c.enabled AND COALESCE(c.backend,'')<>''
 ORDER BY o.info_hash,o.created_at DESC`, userID, hashes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*BYOSObject{}
	for rows.Next() {
		o := &BYOSObject{}
		var data []byte
		if err := rows.Scan(&o.UserID, &o.Bucket, &o.InfoHash, &o.Name, &data); err != nil {
			return nil, err
		}
		if json.Unmarshal(data, &o.Files) == nil && len(o.Files) > 0 {
			out[o.InfoHash] = o
		}
	}
	return out, rows.Err()
}
