package adapter

import (
	"github.com/valkey-io/valkey-go"

	"profile/config"
	"profile/internal/infrastructure/adapter/discount"
	"profile/internal/infrastructure/adapter/discount/payload"
	"profile/internal/infrastructure/gateway"
	"profile/internal/infrastructure/storage"
	"profile/internal/pkg/cache"
)

type Registry struct {
	Discounts *discount.Adapter
}

func NewRegistry(client valkey.Client, stores *storage.Registry, gateways *gateway.Registry) *Registry {
	return &Registry{
		Discounts: discount.NewAdapter(
			stores.Profiles,
			gateways.Pricing,
			config.Instance().Cache,
			cache.NewClient[payload.Discount, *payload.Discount](client),
		),
	}
}
