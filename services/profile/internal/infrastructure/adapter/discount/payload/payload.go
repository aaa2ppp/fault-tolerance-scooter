package payload

import dto "profile/internal/dto/discount"

//go:generate go run github.com/mailru/easyjson/easyjson@v0.9.0 -all payload.go

//easyjson:json
type Discount struct {
	Percent             int32 `json:"discount_percent"`
	CompletedTripsCount int64 `json:"completed_trips_count"`
}

// ConvertFrom сохраняет подтверждённый расчёт из Pricing.
func ConvertFrom(d *dto.Discount) Discount {
	return Discount{Percent: d.Percent, CompletedTripsCount: d.CompletedTripsCount}
}

func (p Discount) ConvertTo() *dto.Discount {
	return &dto.Discount{
		Percent:             p.Percent,
		CompletedTripsCount: p.CompletedTripsCount,
		Source:              "pricing",
	}
}
