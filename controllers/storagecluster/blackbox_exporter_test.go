// blackbox_exporter_test.go
package storagecluster

import (
	"testing"
)

// TestBuildIPRegex verifies that buildIPRegex generates the correct regex string
func TestBuildIPRegex(t *testing.T) {
	tests := []struct {
		name     string
		ips      []string
		expected string
	}{
		{
			name:     "empty list returns never-match regex",
			ips:      []string{},
			expected: "$^",
		},
		{
			name:     "single IPv4 address",
			ips:      []string{"10.0.0.1"},
			expected: "^(10\\.0\\.0\\.1)$",
		},
		{
			name:     "multiple IPv4 addresses",
			ips:      []string{"10.128.2.82", "10.129.2.60", "10.131.0.49"},
			expected: "^(10\\.128\\.2\\.82|10\\.129\\.2\\.60|10\\.131\\.0\\.49)$",
		},
		{
			name:     "IPv6 standard format",
			ips:      []string{"2001:db8::1"},
			expected: "^(2001:db8::1)$",
		},
		{
			name:     "IPv6 loopback",
			ips:      []string{"::1"},
			expected: "^(::1)$",
		},
		{
			name:     "multiple IPv6 addresses",
			ips:      []string{"fe80::1", "2001:db8::1", "::1"},
			expected: "^(fe80::1|2001:db8::1|::1)$",
		},
		{
			name:     "mixed IPv4 and IPv6",
			ips:      []string{"10.0.0.1", "2001:db8::1"},
			expected: "^(10\\.0\\.0\\.1|2001:db8::1)$",
		},
		{
			name:     "IPv4 with various octets",
			ips:      []string{"192.168.1.1", "10.255.255.255", "0.0.0.0"},
			expected: "^(192\\.168\\.1\\.1|10\\.255\\.255\\.255|0\\.0\\.0\\.0)$",
		},
		{
			name:     "IPv6 with zone ID",
			ips:      []string{"fe80::1%eth0"},
			expected: "^(fe80::1%eth0)$",
		},
		{
			name:     "single digit octets",
			ips:      []string{"1.2.3.4"},
			expected: "^(1\\.2\\.3\\.4)$",
		},
		{
			name:     "large IPv6 address",
			ips:      []string{"2001:0db8:85a3:0000:0000:8a2e:0370:7334"},
			expected: "^(2001:0db8:85a3:0000:0000:8a2e:0370:7334)$",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildIPRegex(tt.ips)
			if got != tt.expected {
				t.Errorf("buildIPRegex(%q) = %q, want %q", tt.ips, got, tt.expected)
			}
		})
	}
}
