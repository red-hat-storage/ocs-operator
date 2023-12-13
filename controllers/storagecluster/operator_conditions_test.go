package storagecluster

import (
	"context"

	"github.com/operator-framework/operator-lib/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type stubCondition struct {
}

var _ conditions.Condition = stubCondition{}

func newStubOperatorCondition() conditions.Condition {
	return stubCondition{}
}

func (stubCondition) Get(_ context.Context) (*metav1.Condition, error) {
	return &metav1.Condition{}, nil

}

func (stubCondition) Set(_ context.Context, _ metav1.ConditionStatus, _ ...conditions.Option) error {
	return nil
}
