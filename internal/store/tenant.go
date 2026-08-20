package store

import "context"

type tenantKey struct{}

// WithTenant membungkus context dengan ID tenant aktif. Semua query di
// store menyesuaikan dengan ID ini sehingga data antar-toko terisolasi.
func WithTenant(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, tenantKey{}, id)
}

// TenantID mengambil ID tenant dari context. 0 bila tidak ada (mis. request
// publik pra-login yang belum tahu tenant-nya).
func TenantID(ctx context.Context) int64 {
	id, _ := ctx.Value(tenantKey{}).(int64)
	return id
}

// WithTenantScoped memastikan context memiliki tenant; bila kosong maka
// dikembalikan dengan tenantID yang diberikan (fallback untuk request yang
// tenant-nya baru diketahui di tengah alur).
func WithTenantScoped(ctx context.Context, id int64) context.Context {
	if TenantID(ctx) > 0 {
		return ctx
	}
	return WithTenant(ctx, id)
}
