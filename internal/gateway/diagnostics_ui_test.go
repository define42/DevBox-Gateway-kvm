package gateway

import (
	"net/http"
	"strings"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/buildinfo"
)

func TestDiagnosticsUI(t *testing.T) {
	t.Parallel()
	version := "devbox-kvm " + buildinfo.Current().Version

	t.Run("admin download and version", func(t *testing.T) {
		t.Parallel()
		s, _, raw, _ := adminFixture(t)
		result := webRequest(s, http.MethodGet, "/admin", raw, nil)
		body := result.Body.String()
		if result.Code != http.StatusOK {
			t.Fatalf("admin status = %d: %s", result.Code, body)
		}
		if !strings.Contains(body, `href="/admin/diagnostics?download=1"`) ||
			!strings.Contains(body, "Download diagnostics") {
			t.Fatalf("admin page lacks diagnostics download action: %s", body)
		}
		if !strings.Contains(body, version) {
			t.Fatalf("admin page lacks application version %q: %s", version, body)
		}
	})

	t.Run("owner dashboard download and version", func(t *testing.T) {
		t.Parallel()
		s, _, raw, _ := webFixture(t)
		result := webRequest(s, http.MethodGet, "/", raw, nil)
		body := result.Body.String()
		if result.Code != http.StatusOK {
			t.Fatalf("dashboard status = %d: %s", result.Code, body)
		}
		if !strings.Contains(body, `href="/perf?download=1"`) ||
			!strings.Contains(body, "Download diagnostics") {
			t.Fatalf("dashboard lost owner-scoped diagnostics download action: %s", body)
		}
		if strings.Contains(body, `href="/admin/diagnostics?download=1"`) {
			t.Fatalf("owner dashboard exposes the administrator diagnostics action: %s", body)
		}
		if !strings.Contains(body, version) {
			t.Fatalf("dashboard lacks application version %q: %s", version, body)
		}
	})
}
