package server

import (
	"testing"
)

// test getObcHashedName function
func TestGetObcHashedName(t *testing.T) {
	tests := []struct {
		name                string
		storageConsumerUUID string
		obcName             string
		obcNamespace        string
		expected            string
	}{
		{
			name:                "basic hashed name test",
			storageConsumerUUID: "412b006a-8829-4273-82e2-6b3470640717",
			obcName:             "my-obc",
			obcNamespace:        "my-namespace",
			expected:            "remote-obc-95bd6a203873da85f0a2e4467984a5b8",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getObcHashedName(tt.storageConsumerUUID, tt.obcName, tt.obcNamespace)
			if got != tt.expected {
				t.Fatalf("getObcHashedName() = %q, expected %q", got, tt.expected)
			}
		})
	}
}
