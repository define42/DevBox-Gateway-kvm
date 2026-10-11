package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type progressTestBackend struct {
	*webBackend
	create func(context.Context, string, string, string, string, func(CreateProgress)) (VM, error)
}

func (b *progressTestBackend) CreateWithProgress(
	ctx context.Context, owner, name, hash, imageID string, report func(CreateProgress),
) (VM, error) {
	return b.create(ctx, owner, name, hash, imageID, report)
}

func creationRequest(t *testing.T, raw, csrf string) *http.Request {
	t.Helper()
	form := url.Values{"csrf": {csrf}, "name": {"progress-test"}, "image": {"selected"}, "owner": {"bob"}}
	r := httptest.NewRequest(http.MethodPost, "https://gateway.test/vms", strings.NewReader(form.Encode()))
	r.RemoteAddr = "127.0.0.1:45678"
	r.Header.Set("Origin", "https://gateway.test")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", creationProgressMediaType)
	if raw != "" {
		r.AddCookie(&http.Cookie{
			Name: sessionCookie, Value: raw, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
		})
	}

	return r
}

func TestCreationProgressStreamsBeforeCompletion(t *testing.T) {
	t.Parallel()
	s, base, raw, sess := webFixture(t)
	finish := make(chan struct{})
	s.config.Backend = &progressTestBackend{webBackend: base, create: func(
		ctx context.Context, owner, name, hash, imageID string, report func(CreateProgress),
	) (VM, error) {
		if owner != "alice" || name != "progress-test" || hash != sess.passwordHash || imageID != "selected" {
			return VM{}, ErrAuthentication
		}
		report(CreateProgress{Phase: "copying", CopiedBytes: 42, TotalBytes: 100})
		select {
		case <-ctx.Done():
			return VM{}, ctx.Err()
		case <-finish:
			return VM{ID: "created", Owner: owner, Name: name}, nil
		}
	}}
	server := httptest.NewTLSServer(s.routes())
	defer server.Close()
	r := creationRequest(t, raw, sess.csrf)
	r.URL, _ = url.Parse(server.URL + "/vms")
	r.RequestURI = ""
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.Header.Get("Content-Type") != creationProgressMediaType ||
		response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("stream content type or cache policy is incorrect")
	}
	decoder := json.NewDecoder(response.Body)
	var preparing, copying, complete creationEvent
	if err := decoder.Decode(&preparing); err != nil || preparing.Phase != "preparing" {
		t.Fatalf("initial progress: %+v, %v", preparing, err)
	}
	if err := decoder.Decode(&copying); err != nil || copying.Phase != "copying" ||
		copying.CopiedBytes != 42 || copying.TotalBytes != 100 {
		t.Fatalf("copy progress was unavailable before completion: %+v, %v", copying, err)
	}
	close(finish)
	if err := decoder.Decode(&complete); err != nil || complete.Phase != "complete" {
		t.Fatalf("completion: %+v, %v", complete, err)
	}
	if err := decoder.Decode(&complete); !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected trailing event: %v", err)
	}
}

func TestCreationProgressEnforcesRequestSecurity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, origin       string
		noSession, badCSRF bool
		status             int
	}{
		{name: "missing session", noSession: true, origin: "https://gateway.test", status: http.StatusSeeOther},
		{name: "bad csrf", badCSRF: true, origin: "https://gateway.test", status: http.StatusForbidden},
		{name: "cross origin", origin: "https://other.test", status: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, base, raw, sess := webFixture(t)
			called := false
			s.config.Backend = &progressTestBackend{webBackend: base, create: func(
				context.Context, string, string, string, string, func(CreateProgress),
			) (VM, error) {
				called = true

				return VM{}, nil
			}}
			if tc.noSession {
				raw = ""
			}
			csrf := sess.csrf
			if tc.badCSRF {
				csrf = "invalid"
			}
			r := creationRequest(t, raw, csrf)
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			s.routes().ServeHTTP(w, r)
			if w.Code != tc.status || called {
				t.Fatalf("unauthorized create: status %d, called %t", w.Code, called)
			}
		})
	}
}

func TestCreationProgressFailureDoesNotReportComplete(t *testing.T) {
	t.Parallel()
	s, base, raw, sess := webFixture(t)
	s.config.Backend = &progressTestBackend{webBackend: base, create: func(
		_ context.Context, _, _, _, _ string, report func(CreateProgress),
	) (VM, error) {
		report(CreateProgress{Phase: "copying", CopiedBytes: 50, TotalBytes: 100})

		return VM{}, ErrConflict
	}}
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, creationRequest(t, raw, sess.csrf))
	decoder := json.NewDecoder(w.Body)
	var event creationEvent
	for range 3 {
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event.Phase == "complete" {
			t.Fatal("failed creation reported success")
		}
	}
	if event.Phase != "error" || event.Error == "" {
		t.Fatalf("missing error: %+v", event)
	}
}

type brokenProgressWriter struct {
	*httptest.ResponseRecorder
	writes int
}

func (w *brokenProgressWriter) Write(data []byte) (int, error) {
	w.writes++
	if w.writes > 1 {
		return 0, io.ErrClosedPipe
	}

	return w.ResponseRecorder.Write(data)
}

func TestCreationProgressBrokenStreamCancelsBackend(t *testing.T) {
	t.Parallel()
	s, base, raw, sess := webFixture(t)
	canceled := false
	s.config.Backend = &progressTestBackend{webBackend: base, create: func(
		ctx context.Context, _, _, _, _ string, report func(CreateProgress),
	) (VM, error) {
		report(CreateProgress{Phase: "copying", CopiedBytes: 50, TotalBytes: 100})
		canceled = errors.Is(ctx.Err(), context.Canceled)

		return VM{}, ctx.Err()
	}}
	w := &brokenProgressWriter{ResponseRecorder: httptest.NewRecorder()}
	s.routes().ServeHTTP(w, creationRequest(t, raw, sess.csrf))
	if !canceled {
		t.Fatal("disconnected progress stream did not cancel provisioning")
	}
}

func TestCreationProgressOutlivesOrdinaryHTTPWriteTimeout(t *testing.T) {
	t.Parallel()
	s, base, raw, sess := webFixture(t)
	s.config.CreationTimeout = 2 * time.Second
	s.config.Backend = &progressTestBackend{webBackend: base, create: func(
		ctx context.Context, _, _, _, _ string, report func(CreateProgress),
	) (VM, error) {
		time.Sleep(100 * time.Millisecond)
		report(CreateProgress{Phase: "copying", CopiedBytes: 100, TotalBytes: 100})

		return VM{}, ctx.Err()
	}}
	server := httptest.NewUnstartedServer(s.routes())
	server.Config.WriteTimeout = 25 * time.Millisecond
	server.StartTLS()
	defer server.Close()
	r := creationRequest(t, raw, sess.csrf)
	r.URL, _ = url.Parse(server.URL + "/vms")
	r.RequestURI = ""
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(response.Body)
	for _, phase := range []string{"preparing", "copying", "complete"} {
		var event creationEvent
		if err := decoder.Decode(&event); err != nil || event.Phase != phase {
			t.Fatalf("phase %q: %+v, %v", phase, event, err)
		}
	}
}

func TestCreationDeadlineCancelsBackend(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, base, raw, sess := webFixture(t)
		s.config.CreationTimeout = time.Minute
		var backendErr error
		s.config.Backend = &progressTestBackend{webBackend: base, create: func(
			ctx context.Context, _, _, _, _ string, _ func(CreateProgress),
		) (VM, error) {
			<-ctx.Done()
			backendErr = ctx.Err()

			return VM{}, backendErr
		}}
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, creationRequest(t, raw, sess.csrf))
		if !errors.Is(backendErr, context.DeadlineExceeded) || strings.Contains(w.Body.String(), "complete") {
			t.Fatalf("creation lifetime not bounded: %v, %s", backendErr, w.Body.String())
		}
	})
}

// Model net/http's non-revivable expired write deadline without wall-clock sleeps.
type creationDeadlineWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (w *creationDeadlineWriter) SetWriteDeadline(deadline time.Time) error {
	if !w.deadline.IsZero() && !time.Now().Before(w.deadline) {
		return os.ErrDeadlineExceeded
	}
	w.deadline = deadline

	return nil
}

func TestCreationRestoresDeadlineBetweenProgressWrites(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, base, raw, sess := webFixture(t)
		s.config.CreationTimeout = time.Minute
		s.config.Backend = &progressTestBackend{webBackend: base, create: func(
			ctx context.Context, _, _, _, _ string, report func(CreateProgress),
		) (VM, error) {
			time.Sleep(35 * time.Second)
			report(CreateProgress{Phase: "finalizing"})

			return VM{}, ctx.Err()
		}}
		w := &creationDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
		s.routes().ServeHTTP(w, creationRequest(t, raw, sess.csrf))
		if !strings.Contains(w.Body.String(), `"phase":"complete"`) {
			t.Fatalf("healthy long creation expired between writes: %s", w.Body.String())
		}
	})
}
