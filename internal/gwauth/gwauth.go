// Package gwauth is the gateway-trust helper. When my.lisaos.dev
// (the nginx gateway) proxies a request to a downstream service, it
// validates the caller's token against accounts, writes X-Auth-*
// headers describing the identity, and signs the request with
// X-Internal-Secret (shared via INTERNAL_SHARED_SECRET env var).
//
// Lives inline (one .go file) in each service that needs it rather
// than as a shared module — keeps each repo standalone and side-steps
// private-module Docker-auth headaches.
package gwauth

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

// Identity is the decoded identity the gateway has asserted on a
// request. All fields map directly to X-Auth-* headers.
type Identity struct {
	UserID      string
	Email       string
	Name        string
	Scope       string // "user" | "org"
	OrgID       string
	OrgSlug     string
	Roles       []string
	PublisherID string
}

func (id *Identity) HasRole(role string) bool {
	if id == nil || id.Scope != "org" {
		return false
	}
	for _, r := range id.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// Trusted reports whether the request came through the gateway.
// Used by AdminAuth middleware and oracle's control-plane calls.
func Trusted(r *http.Request) bool {
	secret := os.Getenv("INTERNAL_SHARED_SECRET")
	if secret == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Internal-Secret")), []byte(secret)) == 1
}

// Gateway returns the gateway-asserted identity on the request, or
// nil if the request didn't come through the gateway.
func Gateway(r *http.Request) *Identity {
	if !Trusted(r) {
		return nil
	}
	userID := r.Header.Get("X-Auth-User-ID")
	if userID == "" {
		return nil
	}
	id := &Identity{
		UserID:      userID,
		Email:       r.Header.Get("X-Auth-User-Email"),
		Name:        r.Header.Get("X-Auth-User-Name"),
		Scope:       r.Header.Get("X-Auth-Scope"),
		OrgID:       r.Header.Get("X-Auth-Org-ID"),
		OrgSlug:     r.Header.Get("X-Auth-Org-Slug"),
		PublisherID: r.Header.Get("X-Auth-Publisher-ID"),
	}
	if roles := r.Header.Get("X-Auth-Roles"); roles != "" {
		id.Roles = strings.Split(roles, ",")
	}
	return id
}
