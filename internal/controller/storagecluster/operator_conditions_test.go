package storagecluster

import (
	"context"

	"github.com/operator-framework/operator-lib/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type stubCondition struct {
	status  metav1.ConditionStatus
	reason  string
	message string
}

var _ conditions.Condition = &stubCondition{}

func newStubOperatorCondition() conditions.Condition {
	return &stubCondition{}
}

func (c *stubCondition) Get(_ context.Context) (*metav1.Condition, error) {
	return &metav1.Condition{
		Status:  c.status,
		Reason:  c.reason,
		Message: c.message,
	}, nil
}

func (c *stubCondition) Set(_ context.Context, status metav1.ConditionStatus, opts ...conditions.Option) error {
	cond := &metav1.Condition{Status: status}
	for _, opt := range opts {
		opt(cond)
	}
	c.status = cond.Status
	c.reason = cond.Reason
	c.message = cond.Message
	return nil
}
