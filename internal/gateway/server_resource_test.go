package gateway

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
)

func TestResourceErrorsDoNotExposeHostPaths(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		cause  error
		status int
		text   string
	}{
		{name: "filesystem full", cause: syscall.ENOSPC, status: http.StatusInsufficientStorage, text: "storage"},
		{name: "quota exhausted", cause: syscall.EDQUOT, status: http.StatusInsufficientStorage, text: "quota"},
		{
			name: "cgroup unavailable", cause: ErrCgroupUnavailable,
			status: http.StatusServiceUnavailable, text: "memory and process limits",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := fmt.Errorf("write /private/host/state: %w", test.cause)
			status, message := backendError(err)
			if status != test.status || !strings.Contains(message, test.text) || strings.Contains(message, "/private/") {
				t.Fatalf("backend response = %d, %q", status, message)
			}
			if test.status == http.StatusInsufficientStorage {
				recorder := httptest.NewRecorder()
				imageError(recorder, err)
				body := recorder.Body.String()
				if recorder.Code != test.status || !strings.Contains(body, test.text) || strings.Contains(body, "/private/") {
					t.Fatalf("image response = %d, %q", recorder.Code, body)
				}
			}
		})
	}
}
