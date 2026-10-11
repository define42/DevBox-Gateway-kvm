package gateway

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type adminTestBackend struct {
	*webBackend
	imported, selected, deleted string
	content                     []byte
	images                      []Image
}

func (b *adminTestBackend) ListAll(ctx context.Context) ([]VM, error) { return b.List(ctx, "") }
func (b *adminTestBackend) GetAny(ctx context.Context, id string) (VM, error) {
	vms, err := b.ListAll(ctx)
	if err != nil {
		return VM{}, err
	}
	for _, vm := range vms {
		if vm.ID == id {
			return vm, nil
		}
	}

	return VM{}, ErrNotFound
}

func (b *adminTestBackend) Images(context.Context) ([]Image, error) {
	return append([]Image(nil), b.images...), nil
}

func (b *adminTestBackend) ImportImage(_ context.Context, name string, input io.Reader) (Image, error) {
	content, err := io.ReadAll(input)
	if err != nil {
		return Image{}, err
	}
	b.imported, b.content = name, content
	image := Image{ID: "custom", Name: name}
	b.images = append(b.images, image)

	return image, nil
}

func (b *adminTestBackend) DeleteImage(_ context.Context, id string) error {
	b.deleted = id

	return nil
}

func (b *adminTestBackend) CreateWithImage(ctx context.Context, owner, name, hash, id string) (VM, error) {
	b.selected = id

	return b.Create(ctx, owner, name, hash)
}

func adminFixture(t *testing.T) (*Server, *adminTestBackend, string, browserSession) {
	t.Helper()
	s, base, _, _ := webFixture(t)
	backend := &adminTestBackend{
		webBackend: base,
		images: []Image{
			{ID: "default", Name: "default.qcow2", Default: true},
			{ID: "custom", Name: "Rocky workstation.qcow2"},
		},
	}
	s.config.Backend = backend
	raw, err := s.newIdentitySession(Identity{Username: "alice", IsAdmin: true}, "127.0.0.1", "hash")
	if err != nil {
		t.Fatal(err)
	}
	_, sess, ok := s.store.session(raw, "127.0.0.1")
	if !ok {
		t.Fatal("missing admin session")
	}

	return s, backend, raw, sess
}

func TestAdminAuthorizationAndOwnerBoundRDP(t *testing.T) {
	t.Parallel()
	s, _, raw, sess := webFixture(t)
	for _, endpoint := range []string{"/admin", "/admin/images/default/delete", "/admin/vms/foreign/start"} {
		t.Run(endpoint, func(t *testing.T) {
			t.Parallel()
			method := http.MethodPost
			if endpoint == "/admin" {
				method = http.MethodGet
			}
			result := webRequest(s, method, endpoint, raw, url.Values{"csrf": {sess.csrf}})
			if result.Code != http.StatusForbidden {
				t.Fatalf("non-admin got %d", result.Code)
			}
		})
	}
	a, backend, admin, adminSession := adminFixture(t)
	result := webRequest(a, http.MethodGet, "/admin", admin, nil)
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), "secret-work") {
		t.Fatalf("admin inventory: %d %s", result.Code, result.Body.String())
	}
	for _, action := range []string{"start", "stop", "delete"} {
		result = webRequest(a, http.MethodPost, "/admin/vms/foreign/"+action, admin, url.Values{"csrf": {adminSession.csrf}})
		if result.Code != http.StatusSeeOther {
			t.Fatalf("admin %s: %d", action, result.Code)
		}
		if backend.calls[len(backend.calls)-1] != action+":bob:foreign" {
			t.Fatalf("incorrect owner: %v", backend.calls)
		}
	}
	result = webRequest(a, http.MethodPost, "/admin/vms/foreign/start", admin, url.Values{"csrf": {"wrong"}})
	if result.Code != http.StatusForbidden {
		t.Fatalf("admin CSRF bypass: %d", result.Code)
	}
	for _, endpoint := range []string{"/admin/vms/foreign/rdp", "/vms/foreign/rdp", "/vms/foreign/start"} {
		result = webRequest(a, http.MethodPost, endpoint, admin, url.Values{"csrf": {adminSession.csrf}})
		if result.Code != http.StatusNotFound {
			t.Fatalf("owner boundary bypass %s: %d", endpoint, result.Code)
		}
	}
}

func uploadRequest(
	t *testing.T, s *Server, raw, csrf string, content []byte, extra, truncate bool,
) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("csrf", csrf); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("image", "Rocky.qcow2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if extra {
		if err := writer.WriteField("unexpected", "value"); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	payload := body.Bytes()
	if truncate {
		payload = payload[:len(payload)-20]
	}
	r := httptest.NewRequest(http.MethodPost, "https://gateway.test/admin/images", bytes.NewReader(payload))
	r.RemoteAddr = "127.0.0.1:45678"
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("Origin", "https://gateway.test")
	r.AddCookie(&http.Cookie{
		Name: sessionCookie, Value: raw, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)

	return w
}

func TestAdminUploadStreamsAndRejectsInvalidRequests(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                    string
		invalidCSRF, extra, truncate, oversized bool
		status                                  int
	}{
		{name: "success", status: http.StatusSeeOther},
		{name: "csrf", invalidCSRF: true, status: http.StatusForbidden},
		{name: "extra-part", extra: true, status: http.StatusBadRequest},
		{name: "truncated", truncate: true, status: http.StatusBadRequest},
		{name: "oversized", oversized: true, status: http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, backend, raw, sess := adminFixture(t)
			csrf := sess.csrf
			if tc.invalidCSRF {
				csrf = "incorrect"
			}
			content := bytes.Repeat([]byte("q"), 128<<10)
			if tc.oversized {
				s.config.MaxImageUploadBytes = 1
			}
			result := uploadRequest(t, s, raw, csrf, content, tc.extra, tc.truncate)
			if result.Code != tc.status {
				t.Fatalf("upload: got %d want %d (%s)", result.Code, tc.status, result.Body.String())
			}
			if tc.status == http.StatusSeeOther {
				if backend.imported != "Rocky.qcow2" || !bytes.Equal(backend.content, content) {
					t.Fatal("uploaded file changed")
				}
			} else if backend.imported != "" {
				t.Fatal("rejected upload was published")
			}
		})
	}
}

type forbiddenBody struct{ t *testing.T }

func (b forbiddenBody) Read([]byte) (int, error) {
	b.t.Fatal("read unauthenticated upload body")

	return 0, io.EOF
}
func (forbiddenBody) Close() error { return nil }

func TestAdminUploadAuthorizesBeforeReadingBody(t *testing.T) {
	t.Parallel()
	s, _, raw, _ := webFixture(t)
	for _, token := range []string{"", raw} {
		r := httptest.NewRequest(http.MethodPost, "https://gateway.test/admin/images", nil)
		r.Body = forbiddenBody{t: t}
		r.RemoteAddr = "127.0.0.1:45678"
		r.Header.Set("Origin", "https://gateway.test")
		r.AddCookie(&http.Cookie{
			Name: sessionCookie, Value: token, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
		})
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != http.StatusSeeOther && w.Code != http.StatusForbidden {
			t.Fatalf("authorization status %d", w.Code)
		}
	}
}

func TestDashboardWithEmptyCatalog(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, guidance string
		admin          bool
	}{
		{name: "administrator", admin: true, guidance: `href="/admin#base-images-panel"`},
		{name: "ordinary user", guidance: "Ask an administrator to upload a QCOW2 image"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, backend, _, _ := adminFixture(t)
			backend.images = nil
			raw, err := s.newIdentitySession(Identity{Username: "alice", IsAdmin: tc.admin}, "127.0.0.1", "hash")
			if err != nil {
				t.Fatal(err)
			}
			result := webRequest(s, http.MethodGet, "/", raw, nil)
			body := result.Body.String()
			if result.Code != http.StatusOK || !strings.Contains(body, tc.guidance) ||
				!strings.Contains(body, `id="open-create-button" href="#create-fallback" aria-disabled="true"`) ||
				!strings.Contains(body, `id="create-button" type="submit" disabled`) {
				t.Fatalf("empty catalog did not explain how to enable creation: %d %s", result.Code, body)
			}
			if !strings.Contains(body, "my-work") {
				t.Fatal("empty catalog hid an existing DevBox")
			}
			if !tc.admin && strings.Contains(body, `href="/admin#base-images-panel"`) {
				t.Fatal("ordinary user was directed to an administrator-only upload page")
			}
		})
	}
}

func TestAdminCanUploadFirstImage(t *testing.T) {
	t.Parallel()
	s, backend, raw, sess := adminFixture(t)
	backend.images = nil
	result := webRequest(s, http.MethodGet, "/admin", raw, nil)
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `id="image-upload-form"`) ||
		!strings.Contains(result.Body.String(), "No base images available. Upload an image") {
		t.Fatalf("empty catalog prevented administration: %d %s", result.Code, result.Body.String())
	}
	result = uploadRequest(t, s, raw, sess.csrf, catalogQCOW2("first upload"), false, false)
	if result.Code != http.StatusSeeOther {
		t.Fatalf("first upload failed: %d %s", result.Code, result.Body.String())
	}
	result = webRequest(s, http.MethodGet, "/", raw, nil)
	body := result.Body.String()
	if result.Code != http.StatusOK || !strings.Contains(body, `value="custom"`) ||
		!strings.Contains(body, `id="open-create-button" href="#create-fallback" aria-disabled="false"`) ||
		!strings.Contains(body, `id="empty-images-notice" role="status" hidden`) ||
		strings.Contains(body, `id="create-button" type="submit" disabled`) {
		t.Fatalf("first upload did not enable creation: %d %s", result.Code, body)
	}
	form := url.Values{"csrf": {sess.csrf}, "name": {"first"}, "image": {"custom"}}
	result = webRequest(s, http.MethodPost, "/vms", raw, form)
	if result.Code != http.StatusSeeOther || backend.selected != "custom" || backend.owner != "alice" {
		t.Fatalf("first image was not used for creation: %d %q %q", result.Code, backend.selected, backend.owner)
	}
}

func TestImageSelectionAndAdminLink(t *testing.T) {
	t.Parallel()
	s, backend, raw, sess := adminFixture(t)
	result := webRequest(s, http.MethodGet, "/", raw, nil)
	if !strings.Contains(result.Body.String(), `value="custom"`) ||
		!strings.Contains(result.Body.String(), `href="/admin"`) {
		t.Fatal("dashboard lacks image selection or admin link")
	}
	form := url.Values{"csrf": {sess.csrf}, "name": {"work"}, "image": {"custom"}}
	result = webRequest(s, http.MethodPost, "/vms", raw, form)
	if result.Code != http.StatusSeeOther || backend.selected != "custom" || backend.owner != "alice" {
		t.Fatalf("image selection lost: %d %q %q", result.Code, backend.selected, backend.owner)
	}
	result = webRequest(s, http.MethodPost, "/admin/images/custom/delete", raw, url.Values{"csrf": {sess.csrf}})
	if result.Code != http.StatusSeeOther || backend.deleted != "custom" {
		t.Fatal("image deletion not forwarded")
	}
}

func TestAdminUploadCanceledByLogout(t *testing.T) {
	t.Parallel()
	s, _, raw, _ := adminFixture(t)
	key, _, _ := s.store.session(raw, "127.0.0.1")
	ctx, release, ok := s.uploadContext(context.Background(), key)
	if !ok {
		t.Fatal("cannot bind upload")
	}
	defer release()
	s.store.revoke(key)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("logout did not cancel upload")
	}
	if _, _, ok := s.uploadContext(context.Background(), key); ok {
		t.Fatal("revoked session permitted upload")
	}
}

// A large upload must not inherit the short timeouts used for ordinary forms.
func TestAdminUploadExtendsHTTPDeadlines(t *testing.T) {
	t.Parallel()
	s, backend, raw, sess := adminFixture(t)
	s.config.UploadTimeout = 3 * time.Second
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	if err := writer.WriteField("csrf", sess.csrf); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("image", "slow.qcow2")
	if err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("q"), 128<<10)
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(s.routes())
	server.Config.ReadTimeout = 100 * time.Millisecond
	server.Config.WriteTimeout = 100 * time.Millisecond
	server.StartTLS()
	defer server.Close()
	reader, body := io.Pipe()
	go func() {
		defer body.Close()
		data := payload.Bytes()
		if _, err := body.Write(data[:8192]); err != nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
		_, _ = body.Write(data[8192:])
	}()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/admin/images", reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "gateway.test"
	req.Header.Set("Origin", "https://gateway.test")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(&http.Cookie{
		Name: sessionCookie, Value: raw, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusSeeOther || !bytes.Equal(backend.content, content) {
		t.Fatalf("slow upload failed: %d", response.StatusCode)
	}
}
