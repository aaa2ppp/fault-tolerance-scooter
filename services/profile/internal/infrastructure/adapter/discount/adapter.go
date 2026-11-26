package discount

import (
	"context"

	"github.com/gofrs/uuid"

	"profile/config"
	"profile/internal/domain/entity"
	"profile/internal/domain/loyalty"
	dto "profile/internal/dto/discount"
	"profile/internal/infrastructure/adapter/discount/payload"
	"profile/internal/pkg/cache"
)

type Entry = cache.Entry[payload.Discount, *payload.Discount]

type Storage interface {
	Get(context.Context, uuid.UUID) (*entity.User, error)
}

type Pricing interface {
	CompletedTripsCount(context.Context, uuid.UUID) (int64, error)
}

type Adapter struct {
	profiles Storage
	pricing  Pricing
	cfg      config.Cache
	client   *cache.Client[payload.Discount, *payload.Discount]
}

func NewAdapter(
	profiles Storage,
	pricing Pricing,
	cfg config.Cache,
	client *cache.Client[payload.Discount, *payload.Discount],
) *Adapter {
	return &Adapter{
		profiles: profiles,
		pricing:  pricing,
		cfg:      cfg,
		client:   client,
	}
}

func (a *Adapter) loadSnapshot(ctx context.Context, userID uuid.UUID) (*dto.Discount, error) {
	if _, err := a.profiles.Get(ctx, userID); err != nil {
		return nil, err
	}
	count, err := a.pricing.CompletedTripsCount(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &dto.Discount{
		Percent:             loyalty.DiscountPercent(count),
		CompletedTripsCount: count,
		Source:              "pricing",
	}, nil
}
