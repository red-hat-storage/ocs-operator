package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_getStorageClassClaimName(t *testing.T) {
	type args struct {
		consumerUUID          string
		storageClassClaimName string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "verify get storage class claim name returns expected output",
			args: args{
				consumerUUID:          "consumer-uuid",
				storageClassClaimName: "storage-class-claim-name",
			},
			want: "storageclassclaim-eea7000672540cd95e00f454f8f73e74",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getStorageClassClaimName(tt.args.consumerUUID, tt.args.storageClassClaimName); got != tt.want {
				assert.Equal(t, tt.want, got, "getStorageClassClaimName() = %v, want %v", got, tt.want)
			}
		})
	}
}
