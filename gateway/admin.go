package gateway

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"time"
)

func imageUploadRequest(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/admin/images"
}

func (s *Server) uploadLimit() int64 {
	if s.config.MaxImageUploadBytes > 0 {
		return s.config.MaxImageUploadBytes
	}

	return DefaultMaxImageUploadBytes
}

func (s *Server) uploadTimeout() time.Duration {
	if s.config.UploadTimeout > 0 {
		return s.config.UploadTimeout
	}

	return 30 * time.Minute
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (browserSession, bool) {
	_, sess, ok := s.requireSession(w, r)
	if !ok {
		return sess, false
	}
	if !sess.IsAdmin {
		http.Error(w, "Administrator access is required.", http.StatusForbidden)

		return sess, false
	}

	return sess, true
}

func (s *Server) adminPage(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	backend, ok := s.config.Backend.(AdminBackend)
	images, imagesOK := s.config.Backend.(ImageBackend)
	if !ok || !imagesOK {
		http.Error(w, "Administration is unavailable for this backend.", http.StatusServiceUnavailable)

		return
	}
	vms, err := backend.ListAll(r.Context())
	if err != nil {
		status, message := backendError(err)
		http.Error(w, message, status)

		return
	}
	catalog, err := images.Images(r.Context())
	if err != nil {
		http.Error(w, "Base images are temporarily unavailable.", http.StatusServiceUnavailable)

		return
	}
	s.render(w, http.StatusOK, "admin.html", pageData{
		User: sess.owner, CSRF: sess.csrf, Admin: true, VMs: vms, Images: catalog,
		MaxImageUploadBytes: s.uploadLimit(),
	})
}

func (s *Server) adminVMAction(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	backend, ok := s.config.Backend.(AdminBackend)
	if !ok {
		http.Error(w, "Administration is unavailable for this backend.", http.StatusServiceUnavailable)

		return
	}
	action := r.PathValue("action")
	if action != "start" && action != "stop" && action != "delete" {
		http.NotFound(w, r)

		return
	}
	vm, err := backend.GetAny(r.Context(), r.PathValue("id"))
	if err == nil {
		switch action {
		case "start":
			err = s.config.Backend.Start(r.Context(), vm.Owner, vm.ID)
		case "stop":
			err = s.config.Backend.Stop(r.Context(), vm.Owner, vm.ID)
		case "delete":
			err = s.config.Backend.Delete(r.Context(), vm.Owner, vm.ID)
		}
	}
	if err != nil {
		status, message := backendError(err)
		http.Error(w, message, status)

		return
	}
	// #nosec G706 -- %q escapes every untrusted log field, including control characters.
	log.Printf("gateway admin action: user=%q action=%q vm=%q owner=%q", sess.owner, action, vm.ID, vm.Owner)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) deleteImage(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	backend, ok := s.config.Backend.(ImageBackend)
	if !ok {
		http.Error(w, "Image management is unavailable for this backend.", http.StatusServiceUnavailable)

		return
	}
	if err := backend.DeleteImage(r.Context(), r.PathValue("id")); err != nil {
		imageError(w, err)

		return
	}
	// #nosec G706 -- %q quotes and escapes the image ID; it cannot add log records.
	log.Printf("gateway admin action: user=%q action=delete-image image=%q", sess.owner, r.PathValue("id"))
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) uploadImage(w http.ResponseWriter, r *http.Request) {
	// Check authorization before reading any potentially large request body.
	key, sess, ok := s.requestSession(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)

		return
	}
	if !sess.IsAdmin {
		http.Error(w, "Administrator access is required.", http.StatusForbidden)

		return
	}
	if token := r.Header.Get("X-Csrf-Token"); token != "" && !equalToken(sess.csrf, token) {
		http.Error(w, "Reload the page and try again.", http.StatusForbidden)

		return
	}
	backend, ok := s.config.Backend.(ImageBackend)
	if !ok {
		http.Error(w, "Image management is unavailable for this backend.", http.StatusServiceUnavailable)

		return
	}
	if !s.uploading.CompareAndSwap(false, true) {
		http.Error(w, "Another image is uploading. Please try again when it finishes.", http.StatusConflict)

		return
	}
	defer s.uploading.Store(false)
	if err := s.uploadDeadlines(w); err != nil {
		http.Error(w, "Upload is temporarily unavailable.", http.StatusServiceUnavailable)

		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.uploadLimit()+(64<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Choose a QCOW2 file using the upload form.", http.StatusBadRequest)

		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "csrf" || part.FileName() != "" {
		http.Error(w, "Reload the page and try again.", http.StatusForbidden)

		return
	}
	token, err := io.ReadAll(io.LimitReader(part, 257))
	if err != nil || len(token) > 256 || !equalToken(sess.csrf, string(token)) {
		http.Error(w, "Reload the page and try again.", http.StatusForbidden)

		return
	}
	part, err = reader.NextPart()
	if err != nil || part.FormName() != "image" || part.FileName() == "" {
		http.Error(w, "Choose one QCOW2 image to upload.", http.StatusBadRequest)

		return
	}
	ctx, release, ok := s.uploadContext(r.Context(), key)
	if !ok {
		http.Error(w, "Sign in again before uploading.", http.StatusUnauthorized)

		return
	}
	defer release()
	r = r.WithContext(ctx)
	readCanceled := make(chan struct{})
	stopReadCancel := context.AfterFunc(ctx, func() {
		_ = http.NewResponseController(w).SetReadDeadline(time.Now())
		close(readCanceled)
	})
	defer func() {
		if !stopReadCancel() {
			<-readCanceled
		}
	}()
	input := &imageUploadReader{part: part, multipart: reader, server: s, request: r, session: key}
	//nolint:contextcheck // uploadContext derives from r.Context and binds browser-session cancellation.
	image, err := backend.ImportImage(ctx, part.FileName(), input)
	if err != nil {
		imageError(w, err)

		return
	}
	log.Printf("gateway admin action: user=%q action=upload-image image=%q", sess.owner, image.ID)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// uploadContext binds an import, including conversion, to the browser session.
func (s *Server) uploadContext(ctx context.Context, key [sha256.Size]byte) (context.Context, func(), bool) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	s.store.pruneLocked(time.Now())
	sess := s.store.sessions[key]
	if sess == nil || !sess.IsAdmin {
		return nil, nil, false
	}
	ctx, cancel := context.WithDeadline(ctx, sess.expires)
	lease := &connectionLease{cancel: cancel}
	sess.connections[lease] = struct{}{}

	return ctx, func() {
		cancel()
		s.store.mu.Lock()
		delete(sess.connections, lease)
		s.store.mu.Unlock()
	}, true
}

// Give authenticated uploads their own bounded deadlines; ordinary requests retain
// the short server timeouts. ResponseController unwraps through TLS automatically.
func (s *Server) uploadDeadlines(w http.ResponseWriter) error {
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(s.uploadTimeout())
	for _, err := range []error{controller.SetReadDeadline(deadline), controller.SetWriteDeadline(deadline)} {
		if err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
	}

	return nil
}

// imageUploadReader validates the closing multipart boundary before the importer
// can publish the file, and rejects a session revoked during the upload.
type imageUploadReader struct {
	part      *multipart.Part
	multipart *multipart.Reader
	server    *Server
	request   *http.Request
	session   [sha256.Size]byte
	finished  bool
}

func (r *imageUploadReader) Read(p []byte) (int, error) {
	if err := r.request.Context().Err(); err != nil {
		return 0, err
	}
	if r.finished {
		return 0, io.EOF
	}
	n, err := r.part.Read(p)
	if ctxErr := r.request.Context().Err(); ctxErr != nil {
		return n, ctxErr
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return n, fmt.Errorf("%w: incomplete upload", ErrInvalid)
	}
	if !errors.Is(err, io.EOF) {
		return n, err
	}
	if _, nextErr := r.multipart.NextPart(); !errors.Is(nextErr, io.EOF) {
		return n, errors.Join(ErrInvalid, nextErr)
	}
	key, sess, ok := r.server.requestSession(r.request)
	if !ok || key != r.session || !sess.IsAdmin {
		return n, ErrAuthentication
	}
	r.finished = true

	return n, io.EOF
}

func imageError(w http.ResponseWriter, err error) {
	var maxBytes *http.MaxBytesError
	switch {
	case errors.As(err, &maxBytes), errors.Is(err, ErrImageTooLarge):
		http.Error(w, "The image exceeds the configured upload size limit.", http.StatusRequestEntityTooLarge)
	case errors.Is(err, ErrImageToolUnavailable):
		http.Error(
			w, "This image needs conversion. Install qemu-utils on the gateway host and try again.",
			http.StatusServiceUnavailable,
		)
	case errors.Is(err, ErrAuthentication):
		http.Error(w, "Your session expired. Sign in again before uploading.", http.StatusUnauthorized)
	case errors.Is(err, ErrInvalid):
		http.Error(
			w, "Use a valid standalone QCOW2 image without backing files, encryption, or external data.",
			http.StatusBadRequest,
		)
	case errors.Is(err, ErrNotFound):
		http.Error(w, "That image could not be found. Refresh the page.", http.StatusNotFound)
	case errors.Is(err, ErrConflict):
		http.Error(
			w, "The image is busy, its name is already used, or it is the protected default image.",
			http.StatusConflict,
		)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		http.Error(w, "Image upload timed out or was canceled. Please try again.", http.StatusRequestTimeout)
	default:
		log.Printf("gateway image operation failed: %v", err)
		http.Error(
			w, "Image processing failed. Check available disk space and the server log.",
			http.StatusInternalServerError,
		)
	}
}
