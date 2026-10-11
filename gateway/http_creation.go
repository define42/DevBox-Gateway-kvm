package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

const creationProgressMediaType = "application/x-ndjson"

type creationEvent struct {
	CreateProgress
	Error string `json:"error,omitempty"`
}

// streamCreation keeps the authenticated create request open while the manager
// reports real copy progress. A broken response cancels the in-flight operation.
func (s *Server) streamCreation(
	w http.ResponseWriter, r *http.Request, sess browserSession, name string, backend CreationProgressBackend,
) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	w.Header().Set("Content-Type", creationProgressMediaType)
	w.Header().Set("X-Accel-Buffering", "no")
	encoder := json.NewEncoder(w)
	controller := http.NewResponseController(w)
	send := func(event creationEvent) {
		if ctx.Err() != nil {
			return
		}
		if err := encoder.Encode(event); err != nil {
			cancel()

			return
		}
		if err := controller.Flush(); err != nil {
			cancel()
		}
	}
	send(creationEvent{CreateProgress: CreateProgress{Phase: "preparing"}})
	if ctx.Err() != nil {
		return
	}
	_, err := backend.CreateWithProgress(ctx, sess.owner, name, sess.passwordHash, r.PostForm.Get("image"),
		func(progress CreateProgress) { send(creationEvent{CreateProgress: progress}) })
	if err != nil {
		_, message := backendError(err)
		if errors.Is(err, ErrNotFound) {
			message = "That base image is no longer available. Choose another image."
		}
		send(creationEvent{CreateProgress: CreateProgress{Phase: "error"}, Error: message})

		return
	}
	send(creationEvent{CreateProgress: CreateProgress{Phase: "complete"}})
}
