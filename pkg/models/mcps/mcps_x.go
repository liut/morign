package mcps

import "context"

// IsRemote 判断是否为远程传输类型（SSE 或 Streamable）
func (t TransType) IsRemote() bool {
	return t == TransTypeSSE || t == TransTypeStreamable
}

func (z HeaderCate) Has(o HeaderCate) bool {
	return z&o > 0
}

func (z HeaderCate) HasAuthorization() bool {
	return z.Has(HeaderCateAuthorization)
}

func (z HeaderCate) HasOwnerSession() bool {
	return z.Has(HeaderCateOwnerID) && z.Has(HeaderCateSessionID)
}

type HeaderFunc func(ctx context.Context) map[string]string

type ctxServerNameKey struct{}

func ContextWithServerName(ctx context.Context, sn string) context.Context {
	return context.WithValue(ctx, ctxServerNameKey{}, sn)
}

func ServerNameFromContext(ctx context.Context) string {
	if s, ok := ctx.Value(ctxServerNameKey{}).(string); ok {
		return s
	}
	return ""
}

type ctxChannelKey struct{}

func ContextWithChannel(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, ctxChannelKey{}, name)
}

func ChannelFromContext(ctx context.Context) string {
	if s, ok := ctx.Value(ctxChannelKey{}).(string); ok {
		return s
	}
	return ""
}
