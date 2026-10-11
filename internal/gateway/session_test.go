package gateway

import (
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
