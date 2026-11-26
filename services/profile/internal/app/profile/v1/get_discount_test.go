package v1

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"

	"profile/internal/application/usecase"
	discountuc "profile/internal/application/usecase/profile/discount"
	dto "profile/internal/dto/discount"
	pb "profile/internal/pkg/pb/profile/profile/v1"
)

type fixedDiscount struct{ result dto.Discount }

func (f fixedDiscount) Get(context.Context, uuid.UUID) (*dto.Discount, error) {
	return &f.result, nil
}

func TestCompletedTripsCountPresence(t *testing.T) {
	for _, degraded := range []bool{false, true} {
		handler := New(&usecase.Registry{
			GetDiscount: discountuc.New(fixedDiscount{dto.Discount{Degraded: degraded}}),
		})
		got, err := handler.GetDiscount(t.Context(), &pb.GetDiscountRequest{
			UserId: "704137fe-93b3-4113-8e09-ed1df4d0e0ea",
		})
		if err != nil {
			t.Fatal(err)
		}
		if (got.CompletedTripsCount == nil) != degraded || got.GetCompletedTripsCount() != 0 {
			t.Fatalf("degraded=%t, completed_trips_count=%v", degraded, got.CompletedTripsCount)
		}
	}
}
