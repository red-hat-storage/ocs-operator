package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_getStorageRequestsName(t *testing.T) {
	type args struct {
		consumerUUID       string
		storageRequestName string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "verify get storage class request name returns expected output",
			args: args{
				consumerUUID:       "consumer-uuid",
				storageRequestName: "storage-class-request-name",
			},
			want: "storagerequest-b0e8b1cae6add3a6b331f955874302ce",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getStorageRequestName(tt.args.consumerUUID, tt.args.storageRequestName); got != tt.want {
				assert.Equal(t, tt.want, got, "getStorageRequestName() = %v, want %v", got, tt.want)
			}
		})
	}
}
