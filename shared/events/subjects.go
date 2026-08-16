package events

const (
	JobSubmitted  = "jobs.submitted"
	JobAssigned   = "jobs.assigned"
	JobProgress   = "jobs.progress"
	JobPublishing = "jobs.publishing"
	JobComplete   = "jobs.complete"
	JobFailed     = "jobs.failed"
	JobDeleted    = "jobs.deleted"

	CairnRequested = "cairn.requested"
	ByosRehydrate  = "byos.rehydrate"

	NodeHeartbeat = "nodes.heartbeat"
)
