package singleton

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsMalformed200(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "upstream siderolabs/omni#2642 verbatim",
			err:  errors.New("rpc error: code = Unknown desc = unexpected HTTP status code received from server: 200 (OK); malformed header: missing HTTP content-type"),
			want: true,
		},
		{
			name: "wrapped upstream error",
			err:  fmt.Errorf("release: %w", errors.New("unexpected HTTP status code received from server: 200 (OK); malformed header: missing HTTP content-type")),
			want: true,
		},
		{
			name: "malformed header on non-200 status — not our false positive",
			err:  errors.New("unexpected HTTP status code received from server: 502 (Bad Gateway); malformed header: missing HTTP content-type"),
			want: false,
		},
		{
			name: "200 without malformed marker — real success path, should not reach here",
			err:  errors.New("something else with 200"),
			want: false,
		},
		{
			name: "unrelated error",
			err:  errors.New("context deadline exceeded"),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := isMalformed200(tc.err); got != tc.want {
				t.Fatalf("isMalformed200(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
