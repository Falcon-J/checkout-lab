package reservation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStoreRejectsInvalidInputBeforeDatabase(t *testing.T) {
	s := NewService(nil, time.Minute)
	for _, tc := range []struct {
		key, sku string
		q        int
	}{
		{"short", "book-go", 1}, {"valid-key-12345678", "Book Go", 1},
		{"valid-key-12345678", "book-go", 0}, {"valid-key-12345678", "book-go", 11},
	} {
		_, _, err := s.Create(context.Background(), tc.key, tc.sku, tc.q)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("invalid input %+v returned %v", tc, err)
		}
	}
}
