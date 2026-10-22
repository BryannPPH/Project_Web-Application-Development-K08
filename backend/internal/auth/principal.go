package auth

import "context"

// PlatformRole hanya mengatur kewenangan platform, bukan akses basis data.
type PlatformRole string

const (
	PlatformRoleUser  PlatformRole = "user"
	PlatformRoleAdmin PlatformRole = "platform_admin"
)

func (r PlatformRole) Valid() bool {
	return r == PlatformRoleUser || r == PlatformRoleAdmin
}

// Principal menyimpan identitas terautentikasi tanpa data sensitif.
type Principal struct {
	UserID       string       `json:"id"`
	Username     string       `json:"username"`
	PlatformRole PlatformRole `json:"platformRole"`
}

type principalContextKey struct{}

// WithPrincipal menyimpan principal dalam context.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFromContext mengambil principal dari context.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}
