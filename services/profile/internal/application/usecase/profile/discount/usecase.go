package discount

import (
	"context"

	"github.com/gofrs/uuid"

	dto "profile/internal/dto/discount"
	app "profile/internal/pkg/profileerror"
)

type Adapter interface {
	Get(context.Context, uuid.UUID) (*dto.Discount, error)
}

type UseCase struct {
	adapter Adapter
}

func New(adapter Adapter) *UseCase {
	return &UseCase{adapter: adapter}
}

func (s *UseCase) Execute(ctx context.Context, userID uuid.UUID) (*dto.Discount, error) {
	if userID == uuid.Nil {
		return nil, app.NewInvalidArgumentErr("Идентификатор должен быть UUID")
	}
	return s.adapter.Get(ctx, userID)
}
