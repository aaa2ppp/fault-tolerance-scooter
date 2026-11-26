package discount

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"

	dto "profile/internal/dto/discount"
)

type discountReader struct {
	userID uuid.UUID
	calls  int
}

func (r *discountReader) Get(_ context.Context, id uuid.UUID) (*dto.Discount, error) {
	r.calls++
	r.userID = id
	return &dto.Discount{Percent: 10}, nil
}

func TestGetDiscount(t *testing.T) {
	reader := &discountReader{}
	uc := New(reader)
	if result, err := uc.Execute(t.Context(), uuid.Nil); err == nil || result != nil || reader.calls != 0 {
		t.Fatal("invalid user accepted")
	}
	id := uuid.FromStringOrNil("704137fe-93b3-4113-8e09-ed1df4d0e0ea")
	result, err := uc.Execute(t.Context(), id)
	if err != nil || result.Percent != 10 || reader.userID != id {
		t.Fatalf("result=%v user=%s err=%v", result, reader.userID, err)
	}
}
