package gateway

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

type diskLimitHTTPFixture struct {
	server  *Server
	manager *Manager
	raw     string
	csrf    string
}

func newDiskLimitHTTPFixture(t *testing.T) diskLimitHTTPFixture {
	t.Helper()
	config := managerTestConfig(t, "graceful")
	config.MaxDiskBytes = 4096
	manager, err := NewManager(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})

	server, initialBackend, initialRaw, initialSession := webFixture(t)
	if initialBackend == nil || initialRaw == "" || initialSession.csrf == "" {
		t.Fatal("invalid web fixture")
	}
	server.config.Backend = manager
	raw, err := server.newIdentitySession(
		Identity{Username: "alice", IsAdmin: true}, "127.0.0.1", managerTestPasswordHash(),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, session, ok := server.store.session(raw, "127.0.0.1")
	if !ok {
		t.Fatal("missing session")
	}

	return diskLimitHTTPFixture{server: server, manager: manager, raw: raw, csrf: session.csrf}
}

func TestDiskLimitErrorsAreReadable(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("/private/state/disk.img: %w", ErrDiskTooLarge)
	status, message := backendError(err)
	if status != http.StatusConflict || !strings.Contains(message, "maximum size allowed per VM") ||
		strings.Contains(message, "/private/") {
		t.Fatalf("disk limit response = %d, %q", status, message)
	}
	recorder := httptest.NewRecorder()
	imageError(recorder, err)
	if recorder.Code != http.StatusRequestEntityTooLarge ||
		!strings.Contains(recorder.Body.String(), "virtual disk") ||
		strings.Contains(recorder.Body.String(), "/private/") {
		t.Fatalf("image disk limit response = %d, %q", recorder.Code, recorder.Body.String())
	}
}

func TestDiskLimitHTTPRequests(t *testing.T) {
	t.Parallel()

	t.Run("upload", testDiskLimitUpload)
	t.Run("create", testDiskLimitCreate)
	t.Run("stream create", testDiskLimitStreamCreate)
	t.Run("owner start", testDiskLimitOwnerStart)
	t.Run("admin start", testDiskLimitAdminStart)
}

func testDiskLimitUpload(t *testing.T) {
	t.Parallel()
	fixture := newDiskLimitHTTPFixture(t)
	result := uploadRequest(
		t, fixture.server, fixture.raw, fixture.csrf, catalogQCOW2("oversized"), false, false,
	)
	assertDiskLimitHTTPResponse(t, result, http.StatusRequestEntityTooLarge)
}

func testDiskLimitCreate(t *testing.T) {
	t.Parallel()
	fixture := newDiskLimitHTTPFixture(t)
	growDiskForHTTPTest(t, fixture.manager.config.BaseImage)
	result := webRequest(
		fixture.server, http.MethodPost, "/vms", fixture.raw, diskLimitForm(fixture.csrf),
	)
	assertDiskLimitHTTPResponse(t, result, http.StatusConflict)
}

func testDiskLimitStreamCreate(t *testing.T) {
	t.Parallel()
	fixture := newDiskLimitHTTPFixture(t)
	growDiskForHTTPTest(t, fixture.manager.config.BaseImage)
	request := creationRequest(t, fixture.raw, fixture.csrf)
	encoded := diskLimitForm(fixture.csrf).Encode()
	request.Body = io.NopCloser(strings.NewReader(encoded))
	request.ContentLength = int64(len(encoded))
	result := httptest.NewRecorder()
	fixture.server.routes().ServeHTTP(result, request)
	assertDiskLimitCreationStream(t, result)
}

func testDiskLimitOwnerStart(t *testing.T) {
	t.Parallel()
	fixture := newDiskLimitHTTPFixture(t)
	vm := oversizedManagerVM(t, fixture)
	path := "/vms/" + vm.ID + "/start"
	result := webRequest(
		fixture.server, http.MethodPost, path, fixture.raw, diskLimitForm(fixture.csrf),
	)
	assertDiskLimitHTTPResponse(t, result, http.StatusConflict)
}

func testDiskLimitAdminStart(t *testing.T) {
	t.Parallel()
	fixture := newDiskLimitHTTPFixture(t)
	vm := oversizedManagerVM(t, fixture)
	path := "/admin/vms/" + vm.ID + "/start"
	result := webRequest(
		fixture.server, http.MethodPost, path, fixture.raw, diskLimitForm(fixture.csrf),
	)
	assertDiskLimitHTTPResponse(t, result, http.StatusConflict)
}

func diskLimitForm(csrf string) url.Values {
	return url.Values{"csrf": {csrf}, "name": {"desktop"}, "image": {DefaultImageID}}
}

func growDiskForHTTPTest(t *testing.T, path string) {
	t.Helper()
	var capacity [8]byte
	binary.BigEndian.PutUint64(capacity[:], 8192)
	writeDiskHeaderBytes(t, path, capacity[:], 24)
}

func oversizedManagerVM(t *testing.T, fixture diskLimitHTTPFixture) VM {
	t.Helper()
	vm := createManagerVM(t, fixture.manager, "alice", "desktop")
	// A lower operator limit blocks this previously admitted disk.
	fixture.manager.config.MaxDiskBytes = vm.DiskSizeBytes - 1

	return vm
}

func assertDiskLimitHTTPResponse(t *testing.T, result *httptest.ResponseRecorder, status int) {
	t.Helper()
	if result.Code != status || !strings.Contains(result.Body.String(), "maximum size allowed per VM") {
		t.Fatalf("response = %d, %s", result.Code, result.Body.String())
	}
}

func assertDiskLimitCreationStream(t *testing.T, result *httptest.ResponseRecorder) {
	t.Helper()
	events := decodeCreationEvents(t, result.Body)
	if len(events) == 0 {
		t.Fatal("disk-limit stream returned no events")
	}
	for _, event := range events {
		if event.Phase == "complete" {
			t.Fatal("oversized disk creation reported success")
		}
	}
	last := events[len(events)-1]
	if last.Phase != "error" || !strings.Contains(last.Error, "maximum size allowed per VM") {
		t.Fatalf("disk limit event = %+v", last)
	}
}

func decodeCreationEvents(t *testing.T, input io.Reader) []creationEvent {
	t.Helper()
	decoder := json.NewDecoder(input)
	events := make([]creationEvent, 0, 2)
	for {
		var event creationEvent
		err := decoder.Decode(&event)
		if errors.Is(err, io.EOF) {
			return events
		}
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
}

func TestChangedDiskHTTPRequests(t *testing.T) {
	t.Parallel()

	for _, prefix := range []string{"/vms/", "/admin/vms/"} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			fixture := newDiskLimitHTTPFixture(t)
			vm := createManagerVM(t, fixture.manager, "alice", "desktop")
			growDiskForHTTPTest(t, filepath.Join(fixture.manager.directory(vm.ID), "disk.img"))
			result := webRequest(fixture.server, http.MethodPost, prefix+vm.ID+"/start",
				fixture.raw, diskLimitForm(fixture.csrf))
			if result.Code != http.StatusConflict ||
				!strings.Contains(result.Body.String(), "expected format and capacity") ||
				strings.Contains(result.Body.String(), fixture.manager.config.DataDir) {
				t.Fatalf("changed disk response = %d, %s", result.Code, result.Body.String())
			}
		})
	}
}
