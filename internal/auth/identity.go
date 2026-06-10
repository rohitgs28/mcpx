package auth

import "context"

type contextKey int

const clientKey contextKey = iota

// WithClient returns a context carrying the authenticated client's name.
func WithClient(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, clientKey, name)
}

// ClientFrom returns the authenticated client name from the context, or ""
// when auth is disabled or the auth type carries no identity.
func ClientFrom(ctx context.Context) string {
	if name, ok := ctx.Value(clientKey).(string); ok {
		return name
	}
	return ""
}
