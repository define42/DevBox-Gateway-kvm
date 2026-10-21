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
	// net/http also applies WriteTimeout to the TLS handshake; allow loaded CI
	// enough time to reach the handler before testing the longer creation stream.
	const ordinaryWriteTimeout = time.Second

	s, base, raw, sess := webFixture(t)
	s.config.CreationTimeout = 10 * ordinaryWriteTimeout
	s.config.Backend = &progressTestBackend{webBackend: base, create: func(
		ctx context.Context, _, _, _, _ string, report func(CreateProgress),
	) (VM, error) {
		// Creation must outlive the ordinary response deadline.
		time.Sleep(2 * ordinaryWriteTimeout)
		report(CreateProgress{Phase: "copying", CopiedBytes: 100, TotalBytes: 100})

		return VM{}, ctx.Err()
	}}
	server := httptest.NewUnstartedServer(s.routes())
	server.Config.WriteTimeout = ordinaryWriteTimeout
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
	for _, tc := range []struct {
		name                   string
		timeout, parentTimeout time.Duration
		cancelAfter, want      time.Duration
		wantErr                error
	}{
		{name: "default", want: 30 * time.Minute, wantErr: context.DeadlineExceeded},
		{name: "shorter", timeout: time.Minute, want: time.Minute, wantErr: context.DeadlineExceeded},
		{name: "longer", timeout: 10 * time.Minute, want: 10 * time.Minute, wantErr: context.DeadlineExceeded},
		{
			name: "earlier parent deadline", timeout: 10 * time.Minute, parentTimeout: 45 * time.Second,
			want: 45 * time.Second, wantErr: context.DeadlineExceeded,
		},
		{
			name: "request cancellation", timeout: 10 * time.Minute, cancelAfter: 30 * time.Second,
			want: 30 * time.Second, wantErr: context.Canceled,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				s, base, raw, sess := webFixture(t)
				s.config.CreationTimeout = tc.timeout
				var backendErr error
				s.config.Backend = &progressTestBackend{webBackend: base, create: func(
					ctx context.Context, _, _, _, _ string, _ func(CreateProgress),
				) (VM, error) {
					<-ctx.Done()
					backendErr = ctx.Err()

					return VM{}, backendErr
				}}
				r := creationRequest(t, raw, sess.csrf)
				if tc.parentTimeout != 0 {
					ctx, cancel := context.WithTimeout(r.Context(), tc.parentTimeout)
					defer cancel()
					r = r.WithContext(ctx)
				}
				if tc.cancelAfter != 0 {
					ctx, cancel := context.WithCancel(r.Context())
					defer cancel()
					timer := time.AfterFunc(tc.cancelAfter, cancel)
					defer timer.Stop()
					r = r.WithContext(ctx)
				}
				started := time.Now()
				w := httptest.NewRecorder()
				s.routes().ServeHTTP(w, r)
				if elapsed := time.Since(started); elapsed != tc.want {
					t.Fatalf("creation canceled after %v, want %v", elapsed, tc.want)
				}
				if !errors.Is(backendErr, tc.wantErr) || strings.Contains(w.Body.String(), "complete") {
					t.Fatalf("creation lifetime not bounded: %v, %s", backendErr, w.Body.String())
				}
			})
		})
	}
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
		s.config.CreationTimeout = 10 * time.Minute
		s.config.Backend = &progressTestBackend{webBackend: base, create: func(
			ctx context.Context, _, _, _, _ string, report func(CreateProgress),
		) (VM, error) {
			// Simulate a queued creation followed by a slow copy without progress.
			time.Sleep(3 * time.Minute)
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

// Exercise the ordinary HTML form path as well as streamed provisioning.
type formCreationTestBackend struct {
	*adminTestBackend
	create func(context.Context) (VM, error)
}

func (b *formCreationTestBackend) CreateWithImage(ctx context.Context, _, _, _, _ string) (VM, error) {
	return b.create(ctx)
}

func TestCreationFormOutlivesOrdinaryRequestTimeout(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, base, raw, sess := webFixture(t)
		s.config.CreationTimeout = 10 * time.Minute
		called := false
		s.config.Backend = &formCreationTestBackend{
			adminTestBackend: &adminTestBackend{webBackend: base},
			create: func(ctx context.Context) (VM, error) {
				called = true
				time.Sleep(3 * time.Minute)

				return VM{}, ctx.Err()
			},
		}
		r := creationRequest(t, raw, sess.csrf)
		r.Header.Del("Accept")
		w := &creationDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
		s.routes().ServeHTTP(w, r)
		if !called || w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
			t.Fatalf("long form creation failed: called=%t status=%d", called, w.Code)
		}
	})
}

func TestCreationTimeoutDoesNotExtendOtherRequests(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, method, path string
	}{
		{name: "inventory", method: http.MethodGet, path: "/"},
		{name: "VM action", method: http.MethodPost, path: "/vms/mine/start"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				s, base, raw, sess := webFixture(t)
				s.config.CreationTimeout = 10 * time.Minute
				var backendErr error
				s.config.Backend = &perfReportTestBackend{webBackend: base, list: func(
					ctx context.Context, _ string,
				) ([]VM, error) {
					<-ctx.Done()
					backendErr = ctx.Err()

					return nil, backendErr
				}}
				started := time.Now()
				webRequest(s, tc.method, tc.path, raw, url.Values{"csrf": {sess.csrf}})
				if elapsed := time.Since(started); elapsed != 2*time.Minute ||
					!errors.Is(backendErr, context.DeadlineExceeded) {
					t.Fatalf("ordinary request ended after %v with %v", elapsed, backendErr)
				}
			})
		})
	}
}
