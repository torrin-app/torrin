package events

type Submitted struct {
	JobID string `json:"job_id"`
}

type Assigned struct {
	JobID    string `json:"job_id"`
	InfoHash string `json:"info_hash"`
	Magnet   string `json:"magnet"`
	Source   string `json:"source"`
	MaxBytes int64  `json:"max_bytes"`
	Node     string `json:"node,omitempty"`
}

type Progress struct {
	JobID   string `json:"job_id"`
	Written int64  `json:"written"`
	Total   int64  `json:"total"`
}

type Complete struct {
	JobID    string `json:"job_id"`
	InfoHash string `json:"info_hash"`
	Node     string `json:"node"`
}

type Failed struct {
	JobID  string `json:"job_id"`
	Reason string `json:"reason"`
}

type RehydrateBYOS struct {
	JobID    string `json:"job_id"`
	UserID   string `json:"user_id"`
	InfoHash string `json:"info_hash"`
	Node     string `json:"node,omitempty"`
}

type Deleted struct {
	JobID    string `json:"job_id"`
	InfoHash string `json:"info_hash,omitempty"`
	Source   string `json:"source,omitempty"`
	Node     string `json:"node,omitempty"`
}

type CairnRequest struct {
	InfoHash string `json:"info_hash"`
	Node     string `json:"node"`
}
