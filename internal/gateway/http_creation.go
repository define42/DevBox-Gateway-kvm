package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

const creationProgressMediaType = "application/x-ndjson"

type creationEvent struct {
	CreateProgress
	Error string `json:"error,omitempty"`
}

// streamCreation keeps the authenticated create request open while the manager
// reports real copy progress. A broken response cancels the in-flight operation.
func (s *Server) streamCreation(
	ctx context.Context,
	w http.ResponseWriter, r *http.Request, sess browserSession, name string, backend CreationProgressBackend,
) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	deadline, _ := ctx.Deadline()
	w.Header().Set("Content-Type", creationProgressMediaType)
	w.Header().Set("X-Accel-Buffering", "no")
	encoder := json.NewEncoder(w)
	controller := http.NewResponseController(w)
	send := func(event creationEvent) {
		if ctx.Err() != nil {
			return
		}
		// Bound each network write, but restore the overall deadline between
		// events so a long copy/sync phase does not expire a healthy stream.
		writeDeadline := time.Now().Add(30 * time.Second)
		if deadline.Before(writeDeadline) {
			writeDeadline = deadline
		}
		if err := setCreationWriteDeadline(controller, writeDeadline); err != nil {
			cancel()

			return
		}
		if err := encoder.Encode(event); err != nil {
			cancel()

			return
		}
		if err := controller.Flush(); err != nil {
			cancel()

			return
		}
		if err := setCreationWriteDeadline(controller, deadline); err != nil {
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

// creationContext replaces the ordinary HTTP write deadline only for an
// authenticated create request and propagates its bounded lifetime to the backend.
func (s *Server) creationContext(
	ctx context.Context, w http.ResponseWriter,
) (context.Context, context.CancelFunc, error) {
	timeout := s.config.CreationTimeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	deadline, _ := ctx.Deadline()
	if err := setCreationWriteDeadline(http.NewResponseController(w), deadline); err != nil {
		cancel()

		return nil, nil, err
	}

	return ctx, cancel, nil
}

func setCreationWriteDeadline(controller *http.ResponseController, deadline time.Time) error {
	err := controller.SetWriteDeadline(deadline)
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}

	return err
}
