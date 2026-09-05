package handlers

import (
	"net/http"

	"github.com/torrin-app/torrin/shared/jobs"
)

func admissionStatus(disposition jobs.Admission) int {
	if disposition == jobs.AdmissionExisting {
		return http.StatusOK
	}
	return http.StatusAccepted
}
