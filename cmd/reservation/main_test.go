package main

import "testing"

func TestLocalConfigurationBoundary(t *testing.T) {
	for _, tc := range []struct {
		mode, addr, token string
		ok                bool
	}{
		{"all", "127.0.0.1:8080", "01234567890123456789012345678901", true},
		{"all", "0.0.0.0:8080", "01234567890123456789012345678901", false},
		{"all", "127.0.0.1:8080", "short", false},
		{"unknown", "127.0.0.1:8080", "01234567890123456789012345678901", false},
		{"worker", "127.0.0.1:8080", "", true},
	} {
		err := validateLocalConfig(tc.mode, tc.addr, tc.token)
		if (err == nil) != tc.ok {
			t.Errorf("%+v err=%v", tc, err)
		}
	}
}
