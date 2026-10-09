package reservation

import (
	"errors"
	"regexp"
	"time"
)

var (
	ErrOutOfStock = errors.New("out of stock")
	ErrConflict   = errors.New("idempotency conflict")
	ErrNotFound   = errors.New("not found")
	ErrState      = errors.New("invalid state transition")
	ErrInvalid    = errors.New("invalid request")
	keyPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
	skuPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	idPattern     = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

type Reservation struct {
	ID        string    `json:"id"`
	SKU       string    `json:"sku"`
	Quantity  int       `json:"quantity"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
}

func ValidInput(key, sku string, quantity int) bool {
	return keyPattern.MatchString(key) && skuPattern.MatchString(sku) && quantity >= 1 && quantity <= 10
}

func ValidID(id string) bool { return idPattern.MatchString(id) }
