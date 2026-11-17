package agent

import "net/http"

// Answers the health check with 200 when the runtime can serve requests.
//
// Writes a 503 with the failure when the runtime is unreachable.
func (a *Agent) health(w http.ResponseWriter, r *http.Request) {
	if err := a.runtime.Health(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
