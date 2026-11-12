package httpapi

import "context"

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}
func requestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

type contextProvider interface{ Context() context.Context }

func requestIDFrom(provider contextProvider) string { return requestIDFromContext(provider.Context()) }
