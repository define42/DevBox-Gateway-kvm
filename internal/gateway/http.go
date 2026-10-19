package gateway

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	directory "github.com/go-ldap/ldap/v3"
)

//go:embed login.html dashboard.html dashboard.js admin.html admin.js static
var webAssets embed.FS

type pageData struct {
	User, CSRF, Message                string
	RequiredGroup, BaseImage, MemoryGB string
	CPUs, MemoryMiB                    int
	VMs                                []VM
	Admin                              bool
	Images                             []Image
	MaxImageUploadBytes                int64
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.dashboard)
	mux.HandleFunc("GET /perf", s.perf)
	mux.HandleFunc("GET /admin", s.adminPage)
	mux.HandleFunc("POST /admin/images", s.uploadImage)
	mux.HandleFunc("POST /admin/images/{id}/delete", s.deleteImage)
	mux.HandleFunc("POST /admin/vms/{id}/{action}", s.adminVMAction)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("POST /vms", s.createVM)
	mux.HandleFunc("POST /vms/{id}/{action}", s.vmAction)
	mux.HandleFunc("GET /vms/{id}/performance", s.vmPerformance)
	mux.HandleFunc("GET /static/{asset...}", serveWebAsset)
	mux.HandleFunc("GET /health/latency", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := s.requireSession(w, r); ok {
			w.WriteHeader(http.StatusNoContent)
		}
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		if s.draining {
			s.mu.Unlock()
			http.Error(w, "Gateway is shutting down.", http.StatusServiceUnavailable)

			return
		}
		s.requests.Add(1)
		s.mu.Unlock()
		defer s.requests.Done()
		timeout := 2 * time.Minute
		if imageUploadRequest(r) {
			timeout = s.uploadTimeout()
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		r = r.WithContext(ctx)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set(
			"Content-Security-Policy",
			"default-src 'none'; style-src 'self'; script-src 'self'; font-src 'self'; "+
				"img-src 'self' data:; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'",
		)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		// no-referrer makes browser form POSTs send Origin: null, failing the origin guard below.
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		_, _, hasSession := s.requestSession(r)
		if !hasSession && s.httpConnectionAuthenticated(r) {
			// Even rejected requests cannot keep revoked/expired sessions'
			// authenticated capacity alive on the same socket.
			w.Header().Set("Connection", "close")
			r.Close = true
		}
		if !sameAuthority(r.Host, s.config.PublicAddress) {
			http.Error(w, "Unknown gateway address.", http.StatusMisdirectedRequest)

			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin, err := url.Parse(r.Header.Get("Origin"))
			if err != nil ||
				origin.Scheme != "https" ||
				origin.User != nil ||
				origin.Path != "" ||
				origin.RawQuery != "" ||
				origin.Fragment != "" ||
				!sameAuthority(
					origin.Host,
					s.config.PublicAddress,
				) {
				http.Error(w, "Please submit this form from the gateway page.", http.StatusForbidden)

				return
			}
		}
		// Validate a session before reading a potentially slow request body.
		// TLS, login-page cookies, and public assets never reserve authenticated capacity.
		if hasSession && !s.promoteHTTPConnection(r) {
			connectionBusy(w, r)

			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if imageUploadRequest(r) {
				mux.ServeHTTP(w, r)

				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Invalid form.", http.StatusBadRequest)

				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

// serveWebAsset exposes only the embedded frontend resources, never templates or host files.
func serveWebAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("asset")
	var contentType string
	switch path.Ext(name) {
	case ".css":
		contentType = "text/css; charset=utf-8"
	case ".js":
		contentType = "text/javascript; charset=utf-8"
	case ".woff":
		contentType = "font/woff"
	case ".woff2":
		contentType = "font/woff2"
	default:
		http.NotFound(w, r)

		return
	}
	file := "static/" + name
	if name == "dashboard.js" || name == "admin.js" {
		file = name
	}
	data, err := webAssets.ReadFile(file)
	if err != nil {
		http.NotFound(w, r)

		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	// #nosec G705 -- Immutable embedded CSS/JS/fonts, fixed MIME types, and nosniff; no reflected request content.
	_, _ = w.Write(data)
}

func sameAuthority(a, b string) bool {
	normalize := func(s string) string {
		host, port, err := net.SplitHostPort(s)
		if err != nil {
			if strings.Contains(s, ":") {
				return ""
			}
			host, port = s, "443"
		}

		return net.JoinHostPort(strings.ToLower(host), port)
	}

	return normalize(a) != "" && normalize(a) == normalize(b)
}

func (s *Server) render(w http.ResponseWriter, status int, file string, data pageData) {
	data.RequiredGroup = s.config.RequiredGroup
	if dn, err := directory.ParseDN(data.RequiredGroup); err == nil &&
		len(dn.RDNs) > 0 && len(dn.RDNs[0].Attributes) > 0 {
		data.RequiredGroup = dn.RDNs[0].Attributes[0].Value
	}
	data.BaseImage = s.config.DesktopImage
	data.CPUs, data.MemoryMiB = s.config.DesktopCPUs, s.config.DesktopMemoryMiB
	if data.MemoryMiB > 0 {
		data.MemoryGB = strconv.FormatFloat(float64(data.MemoryMiB)/1024, 'f', -1, 64)
	}
	t, err := template.ParseFS(webAssets, file)
	if err != nil {
		http.Error(w, "Page unavailable.", http.StatusInternalServerError)

		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = t.Execute(w, data)
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requestSession(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)

		return
	}
	s.showLogin(w, http.StatusOK, "")
}

func (s *Server) showLogin(w http.ResponseWriter, status int, message string) {
	token, err := randomToken()
	if err != nil {
		http.Error(w, "Sign in is temporarily unavailable.", http.StatusServiceUnavailable)

		return
	}
	http.SetCookie(
		w,
		&http.Cookie{
			Name:     loginCookie,
			Value:    token,
			Path:     "/",
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   600,
		},
	)
	s.render(w, status, "login.html", pageData{CSRF: token, Message: message})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(loginCookie)
	if err != nil || !equalToken(cookie.Value, r.PostForm.Get("csrf")) {
		s.showLogin(w, http.StatusForbidden, "Reload this page and sign in again.")

		return
	}
	ip := clientIP(r.RemoteAddr)
	if !s.store.allowLogin(ip) {
		s.showLogin(w, http.StatusTooManyRequests, "Too many attempts. Please wait a minute and try again.")

		return
	}
	select {
	case s.authSlots <- struct{}{}:
		defer func() { <-s.authSlots }()
	default:
		s.showLogin(w, http.StatusServiceUnavailable, "Sign in is busy. Please try again shortly.")

		return
	}
	username, password := r.PostForm.Get("username"), r.PostForm.Get("password")
	defer func() { r.PostForm.Del("password"); r.Form.Del("password") }()
	if username == "" || len(username) > 128 || password == "" || len(password) > 1024 {
		s.showLogin(w, http.StatusUnauthorized, "Check your username and password.")

		return
	}
	identity, err := authenticateIdentity(r.Context(), s.config.Authenticator, username, password)
	owner := identity.Username
	if errors.Is(err, ErrInvalid) {
		s.showLogin(
			w,
			http.StatusForbidden,
			"Your directory username is not a supported Linux account name. Contact your administrator.",
		)

		return
	}
	if err != nil {
		s.showLogin(w, http.StatusUnauthorized, "Check your username and password.")

		return
	}
	if !validOwner(owner) {
		s.showLogin(
			w,
			http.StatusForbidden,
			"Your directory username is not a supported Linux account name. Contact your administrator.",
		)

		return
	}
	hash, err := GuestPasswordHash(password)
	// Do not retain the submitted password in request form maps.
	r.PostForm.Del("password")
	r.Form.Del("password")
	if err != nil || r.Context().Err() != nil {
		s.showLogin(w, http.StatusServiceUnavailable, "Sign in is temporarily unavailable.")

		return
	}
	if !s.promoteHTTPConnection(r) {
		connectionBusy(w, r)

		return
	}
	if key, _, ok := s.requestSession(r); ok {
		s.store.revoke(key)
	}
	token, err := s.newLoginSession(identity, username, ip, hash)
	if err != nil {
		s.showLogin(w, http.StatusServiceUnavailable, "Sign in is temporarily unavailable.")

		return
	}
	http.SetCookie(
		w,
		&http.Cookie{
			Name:     sessionCookie,
			Value:    token,
			Path:     "/",
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			MaxAge: int(
				s.config.SessionTTL.Seconds(),
			),
		},
	)
	http.SetCookie(
		w,
		&http.Cookie{
			Name:     loginCookie,
			Path:     "/",
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   -1,
		},
	)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func equalToken(a, b string) bool {
	_, validA := tokenKey(a)
	_, validB := tokenKey(b)

	return validA && validB && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (s *Server) requestSession(r *http.Request) ([sha256.Size]byte, browserSession, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return [sha256.Size]byte{}, browserSession{}, false
	}

	return s.store.session(cookie.Value, clientIP(r.RemoteAddr))
}

func (s *Server) requireSession(w http.ResponseWriter, r *http.Request) ([sha256.Size]byte, browserSession, bool) {
	key, sess, ok := s.requestSession(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)

		return key, sess, false
	}
	if r.Method == http.MethodPost && !equalToken(sess.csrf, r.PostForm.Get("csrf")) {
		http.Error(w, "Reload the page and try again.", http.StatusForbidden)

		return key, sess, false
	}

	return key, sess, true
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	_, sess, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	data, err := s.dashboardData(r, sess)
	status := http.StatusOK
	if err != nil {
		status, data.Message = backendError(err)
	}
	s.render(w, status, "dashboard.html", data)
}

func (s *Server) dashboardData(r *http.Request, sess browserSession) (pageData, error) {
	data := pageData{User: sess.owner, CSRF: sess.csrf, Admin: sess.IsAdmin}
	vms, err := s.config.Backend.List(r.Context(), sess.owner)
	if err != nil {
		return data, err
	}
	for _, vm := range vms {
		if vm.Owner == sess.owner {
			data.VMs = append(data.VMs, vm)
		}
	}
	if images, supported := s.config.Backend.(ImageBackend); supported {
		data.Images, err = images.Images(r.Context())
	}

	return data, err
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	key, _, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	s.store.revoke(key)
	http.SetCookie(
		w,
		&http.Cookie{
			Name:     sessionCookie,
			Path:     "/",
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   -1,
		},
	)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) createVM(w http.ResponseWriter, r *http.Request) {
	_, sess, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if name == "" || len(name) > 63 {
		http.Error(
			w,
			"Choose a desktop name using 1–63 lowercase letters, numbers, or hyphens.",
			http.StatusBadRequest,
		)

		return
	}
	ctx, cancel, err := s.creationContext(r.Context(), w)
	if err != nil {
		http.Error(w, "Cannot start desktop creation.", http.StatusInternalServerError)

		return
	}
	defer cancel()
	if backend, supported := s.config.Backend.(CreationProgressBackend); supported &&
		r.Header.Get("Accept") == creationProgressMediaType {
		s.streamCreation(ctx, w, r, sess, name, backend)

		return
	}

	if images, supported := s.config.Backend.(ImageBackend); supported {
		_, err = images.CreateWithImage(ctx, sess.owner, name, sess.passwordHash, r.PostForm.Get("image"))
	} else if r.PostForm.Get("image") != "" {
		err = ErrInvalid
	} else {
		_, err = s.config.Backend.Create(ctx, sess.owner, name, sess.passwordHash)
	}
	if err != nil {
		status, message := backendError(err)
		if errors.Is(err, ErrNotFound) {
			message = "That base image is no longer available. Choose another image."
		}
		data, _ := s.dashboardData(r, sess)
		data.Message = message
		s.render(w, status, "dashboard.html", data)

		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) ownedVM(r *http.Request, owner, id string) (VM, error) {
	vms, err := s.config.Backend.List(r.Context(), owner)
	if err != nil {
		return VM{}, err
	}
	for _, vm := range vms {
		if vm.ID == id && vm.Owner == owner {
			return vm, nil
		}
	}

	return VM{}, ErrNotFound
}

func (s *Server) vmAction(w http.ResponseWriter, r *http.Request) {
	key, sess, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	vm, err := s.ownedVM(r, sess.owner, r.PathValue("id"))
	if err != nil {
		status, message := backendError(err)
		http.Error(w, message, status)

		return
	}
	switch r.PathValue("action") {
	case "start":
		err = s.config.Backend.Start(r.Context(), sess.owner, vm.ID)
	case "stop":
		err = s.config.Backend.Stop(r.Context(), sess.owner, vm.ID)
	case "delete":
		err = s.config.Backend.Delete(r.Context(), sess.owner, vm.ID)
	case "rdp":
		s.downloadRDP(w, r, key, sess, vm)

		return
	default:
		http.NotFound(w, r)

		return
	}
	if err != nil {
		status, message := backendError(err)
		http.Error(w, message, status)

		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) downloadRDP(
	w http.ResponseWriter,
	r *http.Request,
	key [sha256.Size]byte,
	sess browserSession,
	vm VM,
) {
	if _, err := s.config.Backend.RDP(r.Context(), sess.owner, vm.ID); err != nil {
		status, message := backendError(err)
		http.Error(w, message, status)

		return
	}
	token, err := s.store.grant(key, vm.ID)
	if err != nil {
		http.Error(w, "Sign in again to connect.", http.StatusUnauthorized)

		return
	}
	w.Header().Set("Content-Type", "application/x-rdp")
	w.Header().Set("Content-Disposition", `attachment; filename="desktop.rdp"`)
	_, _ = fmt.Fprintf(
		w, "full address:s:%s\r\nusername:s:%s\r\nloadbalanceinfo:s:%s\r\n"+
			"screen mode id:i:2\r\nuse multimon:i:0\r\nsession bpp:i:32\r\nauthentication level:i:2\r\n"+
			"enablecredsspsupport:i:0\r\nnegotiate security layer:i:1\r\nprompt for credentials:i:1\r\n"+
			"prompt for credentials on client:i:1\r\n"+
			"autoreconnection enabled:i:0\r\nredirectclipboard:i:1\r\n"+
			"audiomode:i:0\r\naudiocapturemode:i:0\r\n",
		s.config.PublicAddress, sess.login, token,
	)
}
