package agent

import "net/http"

// Answers the liveness probe with 200 and no body.
func (a *Agent) health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
