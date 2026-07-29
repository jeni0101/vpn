package webapp

import (
	"context"

	"github.com/jeni0101/vpn/server/internal/auth"
)

type sessionKey struct{}

func withSession(ctx context.Context, session auth.Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, session)
}

func sessionFrom(r interface{ Context() context.Context }) auth.Session {
	session, _ := r.Context().Value(sessionKey{}).(auth.Session)
	return session
}
