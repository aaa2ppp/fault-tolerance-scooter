package v1

import (
	"context"

	"github.com/gofrs/uuid"

	pb "profile/internal/pkg/pb/profile/profile/v1"
)

func (i *Implementation) GetDiscount(ctx context.Context, request *pb.GetDiscountRequest) (*pb.GetDiscountResponse, error) {
	result, err := i.usecases.GetDiscount.Execute(ctx, uuid.FromStringOrNil(request.GetUserId()))
	if err != nil {
		return nil, err
	}
	response := &pb.GetDiscountResponse{
		DiscountPercent: result.Percent,
		Degraded:        result.Degraded,
		Source:          result.Source,
	}
	if !result.Degraded {
		response.CompletedTripsCount = &result.CompletedTripsCount
	}
	return response, nil
}
