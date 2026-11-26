package payload

import "github.com/gofrs/uuid"

func DiscountKey(userID uuid.UUID) string {
	return "user:{" + userID.String() + "}:discount"
}
