package discount

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gofrs/uuid"
	"github.com/valkey-io/valkey-go"

	dto "profile/internal/dto/discount"
	"profile/internal/infrastructure/adapter/discount/payload"
)

func (a *Adapter) Get(ctx context.Context, userID uuid.UUID) (*dto.Discount, error) {
	key := payload.DiscountKey(userID)

	entry, err := a.client.Get(ctx, key)
	if err == nil {
		return entry.Value.ConvertTo(), nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if !errors.Is(err, valkey.Nil) {
		slog.WarnContext(ctx, "Не удалось прочитать скидку из кеша", "error", err)
	}

	snapshot, err := a.loadSnapshot(ctx, userID)
	if err != nil {
		return nil, err
	}

	a.setInBackground(ctx, Entry{
		Key:        key,
		Value:      payload.ConvertFrom(snapshot),
		Expiration: a.cfg.TTL,
	})
	return snapshot, nil
}

// Запоздавший SET может перезаписать более свежую скидку.
func (a *Adapter) setInBackground(ctx context.Context, entry Entry) {
	go func() {
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		if err := a.client.Set(writeCtx, entry); err != nil {
			slog.WarnContext(writeCtx, "Не удалось записать скидку в кеш", "error", err)
		}
	}()
}
