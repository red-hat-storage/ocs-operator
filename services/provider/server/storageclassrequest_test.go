package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_getStorageClassRequestsName(t *testing.T) {
	type args struct {
		consumerUUID            string
		storageClassRequestName string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "verify get storage class request name returns expected output",
			args: args{
				consumerUUID:            "consumer-uuid",
				storageClassRequestName: "storage-class-request-name",
			},
			want: "storageclassrequest-425d23ea878bfdff4fed77389d122af8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getStorageClassRequestName(tt.args.consumerUUID, tt.args.storageClassRequestName); got != tt.want {
				assert.Equal(t, tt.want, got, "getStorageClassRequestName() = %v, want %v", got, tt.want)
			}
		})
	}
}
