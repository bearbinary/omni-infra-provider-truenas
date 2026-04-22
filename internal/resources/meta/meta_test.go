package meta

import (
	"strings"
	"testing"
)

func TestBuildVMName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		providerID string
		requestID  string
		want       string
	}{
		// Default provider id.
		{"truenas", "11111111-2222-3333-4444-555555555555", "omni_truenas_11111111_2222_3333_4444_555555555555"},
		// Dash-underscore collapse.
		{"my-provider", "req-abc-123", "omni_my_provider_req_abc_123"},
		// Invalid chars in providerID replaced.
		{"foo/bar.baz", "x-y", "omni_foo_bar_baz_x_y"},
	}

	for _, tc := range cases {
		got := BuildVMName(tc.providerID, tc.requestID)
		if got != tc.want {
			t.Errorf("BuildVMName(%q, %q) = %q, want %q", tc.providerID, tc.requestID, got, tc.want)
		}
	}
}

// TestBuildVMName_EdgeCases pins behavior on inputs that historically caused
// regressions: unicode sanitization, collapse of consecutive underscores,
// empty strings, and strings that look like the v0.14 shape.
func TestBuildVMName_EdgeCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		providerID string
		requestID  string
		wantPrefix string
		wantSub    string
	}{
		{
			name:       "unicode provider id sanitized to underscores",
			providerID: "café-🔥",
			requestID:  "req-1",
			wantPrefix: "omni_",
			// unicode rune replacement + trailing dash → collapsed underscores.
			// We assert on presence of request-id and absence of raw unicode.
			wantSub: "req_1",
		},
		{
			name:       "empty provider id produces collapsible underscores",
			providerID: "",
			requestID:  "req-1",
			wantPrefix: "omni_",
			wantSub:    "req_1",
		},
		{
			name:       "provider id with only invalid chars sanitizes entirely",
			providerID: "...//..",
			requestID:  "req-1",
			wantPrefix: "omni_",
			wantSub:    "req_1",
		},
		{
			name:       "very long request id preserved",
			providerID: "p",
			requestID:  "00000000-1111-2222-3333-444444444444-extra-suffix",
			wantPrefix: "omni_p_",
			wantSub:    "444444444444_extra_suffix",
		},
		{
			name:       "provider id that looks like legacy v0.14 prefix",
			providerID: "truenas",
			requestID:  "abc",
			wantPrefix: "omni_truenas_",
			wantSub:    "abc",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := BuildVMName(tc.providerID, tc.requestID)

			if !strings.HasPrefix(got, tc.wantPrefix) {
				t.Errorf("BuildVMName(%q, %q) = %q, want prefix %q", tc.providerID, tc.requestID, got, tc.wantPrefix)
			}

			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("BuildVMName(%q, %q) = %q, want contains %q", tc.providerID, tc.requestID, got, tc.wantSub)
			}

			// No raw unicode or invalid characters should ever appear in the result.
			for _, r := range got {
				switch {
				case r >= 'a' && r <= 'z':
				case r >= 'A' && r <= 'Z':
				case r >= '0' && r <= '9':
				case r == '_':
				default:
					t.Errorf("BuildVMName(%q, %q) = %q contains invalid rune %q", tc.providerID, tc.requestID, got, r)
				}
			}

			// Consecutive underscores must be collapsed — TrueNAS VM lists look
			// untidy with them, and a future exact-match check on the name
			// would otherwise diverge based on provider-id punctuation.
			if strings.Contains(got, "__") {
				t.Errorf("BuildVMName(%q, %q) = %q contains consecutive underscores", tc.providerID, tc.requestID, got)
			}
		})
	}
}

func TestParseRequestIDFromDescription(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		desc string
		want string
	}{
		{
			name: "canonical v0.15+ description",
			desc: "Managed by Omni infra provider (request-id: talos-preview-control-planes-abc123)",
			want: "talos-preview-control-planes-abc123",
		},
		{
			name: "legacy v0.14 description (no request-id suffix)",
			desc: "Managed by Omni infra provider",
			want: "",
		},
		{
			name: "empty",
			desc: "",
			want: "",
		},
		{
			name: "description without marker",
			desc: "some unrelated description",
			want: "",
		},
		{
			name: "marker present but unclosed",
			desc: "Managed by Omni infra provider (request-id: broken",
			want: "",
		},
		{
			name: "request-id contains numbers and dashes",
			desc: "Managed by Omni infra provider (request-id: 019db6a1-8974-7bd9-a689-509a79254f09)",
			want: "019db6a1-8974-7bd9-a689-509a79254f09",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := ParseRequestIDFromDescription(tc.desc); got != tc.want {
				t.Errorf("ParseRequestIDFromDescription(%q) = %q, want %q", tc.desc, got, tc.want)
			}
		})
	}
}

func TestIsOmniVMName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		want bool
	}{
		{"omni_truenas_abc", true},
		{"omni_abc", true}, // legacy v0.14 shape
		{"not-ours", false},
		{"", false},
		{"omni", false}, // no underscore after prefix
	}

	for _, tc := range cases {
		if got := IsOmniVMName(tc.name); got != tc.want {
			t.Errorf("IsOmniVMName(%q) = %t, want %t", tc.name, got, tc.want)
		}
	}
}
