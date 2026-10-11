package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAdministratorSessionKeepsVerifiedIdentity(t *testing.T) {
	t.Parallel()
	s := &Server{config: Config{SessionTTL: time.Hour, MaxSessions: 16}, store: newSessionStore()}
	identity := Identity{Username: "operator", IsAdmin: true}
	token, err := s.newIdentitySession(identity, "127.0.0.1", "guest-hash")
	if err != nil {
		t.Fatal(err)
	}
	key, session, ok := s.store.session(token, "127.0.0.1")
	if !ok || session.owner != identity.Username || !session.IsAdmin {
		t.Fatalf("verified administrator role was not preserved: %#v, %v", session, ok)
	}
	s.store.revoke(key)
	if _, _, ok := s.store.session(token, "127.0.0.1"); ok {
		t.Fatal("revoked administrator session remains active")
	}
	ordinary, err := s.newSession("administrator", "127.0.0.1", "guest-hash")
	if err != nil {
		t.Fatal(err)
	}
	_, session, ok = s.store.session(ordinary, "127.0.0.1")
	if !ok || session.IsAdmin {
		t.Fatal("ordinary session inferred administrator access from account name")
	}
}

func TestLoginSessionPreservesSafeDirectoryLogin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, login string
		valid       bool
	}{
		{name: "directory alias", login: "Alice.Login", valid: true},
		{name: "empty"},
		{name: "too long", login: strings.Repeat("a", 129)},
		{name: "carriage return", login: "alice\rusername:s:bob"},
		{name: "newline", login: "alice\npassword:s:injected"},
		{name: "null", login: "alice\x00bob"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, _, _, _ := webFixture(t)
			raw, err := s.newLoginSession(Identity{Username: "alice"}, tc.login, "127.0.0.1", "guest-hash")
			if !tc.valid {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("unsafe RDP login accepted: %v", err)
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			_, sess, ok := s.store.session(raw, "127.0.0.1")
			if !ok || sess.owner != "alice" || sess.login != tc.login {
				t.Fatal("session did not preserve separate login and canonical owner")
			}
		})
	}
}

func TestGrantMustStillBeValidAfterPasswordValidation(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"expired", "replaced", "logout"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, _, raw, _ := webFixture(t)
			key, _, _ := s.store.session(raw, "127.0.0.1")
			token, err := s.store.grant(key, "mine")
			if err != nil {
				t.Fatal(err)
			}
			_, _, _, release, ok := s.store.pendingGrant(context.Background(), token, "127.0.0.1")
			if !ok {
				t.Fatal("valid pending grant was rejected")
			}
			defer release()
			switch name {
			case "expired":
				verifier, _ := tokenKey(token)
				s.store.mu.Lock()
				grant := s.store.grants[verifier]
				grant.expires = time.Now().Add(-time.Second)
				s.store.grants[verifier] = grant
				s.store.mu.Unlock()
			case "replaced":
				if _, err := s.store.grant(key, "mine"); err != nil {
					t.Fatal(err)
				}
			case "logout":
				s.store.revoke(key)
			}
			if s.store.claimGrant(token, "127.0.0.1") {
				t.Fatal("grant invalidated during authentication reached the desktop")
			}
		})
	}
}
