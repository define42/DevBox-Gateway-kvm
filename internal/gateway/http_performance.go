package gateway

import (
	"encoding/json"
	"net/http"
)

func (s *Server) vmPerformance(w http.ResponseWriter, r *http.Request) {
	_, session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	vm, err := s.ownedVM(r, session.owner, r.PathValue("id"))
	if err != nil {
		status, message := backendError(err)
		http.Error(w, message, status)

		return
	}
	performance := VMPerformance{State: "unavailable", GuestMemoryBytes: uint64(s.config.DesktopMemoryMiB) << 20}
	switch vm.State {
	case "stopped":
		performance.State = "stopped"
	case "creating", "starting":
		performance.State = "starting"
	}
	if backend, ok := s.config.Backend.(PerformanceBackend); ok {
		performance, err = backend.Performance(r.Context(), session.owner, vm.ID)
		if err != nil {
			status, message := backendError(err)
			http.Error(w, message, status)

			return
		}
	}
	data, err := json.Marshal(performance)
	if err != nil {
		http.Error(w, "Performance is temporarily unavailable.", http.StatusServiceUnavailable)

		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}
