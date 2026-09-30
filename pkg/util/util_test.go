package util

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsForbiddenError(t *testing.T) {
	cases := []struct {
		label    string
		err      error
		expected bool
	}{
		{
			label:    "nil error",
			err:      nil,
			expected: false,
		},
		{
			label:    "non-status error",
			err:      errors.New("some error"),
			expected: false,
		},
		{
			label: "status error with forbidden cause",
			err: &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Details: &metav1.StatusDetails{
						Causes: []metav1.StatusCause{
							{
								Type:    metav1.CauseTypeForbidden,
								Message: "field is forbidden",
								Field:   "spec.field",
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			label: "status error with multiple causes including forbidden",
			err: &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Details: &metav1.StatusDetails{
						Causes: []metav1.StatusCause{
							{
								Type:    metav1.CauseTypeFieldValueRequired,
								Message: "field is required",
								Field:   "spec.field1",
							},
							{
								Type:    metav1.CauseTypeForbidden,
								Message: "field is forbidden",
								Field:   "spec.field2",
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			label: "status error without forbidden cause",
			err: &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Details: &metav1.StatusDetails{
						Causes: []metav1.StatusCause{
							{
								Type:    metav1.CauseTypeFieldValueRequired,
								Message: "field is required",
								Field:   "spec.field",
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			label: "status error with empty causes",
			err: &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Details: &metav1.StatusDetails{
						Causes: []metav1.StatusCause{},
					},
				},
			},
			expected: false,
		},
		{
			label: "status error with nil details",
			err: &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Details: nil,
				},
			},
			expected: false,
		},
	}

	for i, c := range cases {
		t.Logf("Case %d: %s\n", i+1, c.label)
		result := IsForbiddenError(c.err)
		assert.Equal(t, c.expected, result)
	}
}

func TestIsFieldImmutable(t *testing.T) {
	cases := []struct {
		label    string
		err      error
		expected bool
	}{
		{
			label:    "nil error",
			err:      nil,
			expected: false,
		},
		{
			label:    "non-status error",
			err:      errors.New("some error"),
			expected: false,
		},
		{
			label: "status error with field value invalid cause",
			err: &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Details: &metav1.StatusDetails{
						Causes: []metav1.StatusCause{
							{
								Type:    metav1.CauseTypeFieldValueInvalid,
								Message: "field is immutable",
								Field:   "spec.driver",
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			label: "status error with multiple causes including field value invalid",
			err: &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Details: &metav1.StatusDetails{
						Causes: []metav1.StatusCause{
							{
								Type:    metav1.CauseTypeFieldValueRequired,
								Message: "field is required",
								Field:   "spec.field1",
							},
							{
								Type:    metav1.CauseTypeFieldValueInvalid,
								Message: "field is immutable",
								Field:   "spec.driver",
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			label: "status error without field value invalid cause",
			err: &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Details: &metav1.StatusDetails{
						Causes: []metav1.StatusCause{
							{
								Type:    metav1.CauseTypeFieldValueRequired,
								Message: "field is required",
								Field:   "spec.field",
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			label: "status error with forbidden cause (not field value invalid)",
			err: &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Details: &metav1.StatusDetails{
						Causes: []metav1.StatusCause{
							{
								Type:    metav1.CauseTypeForbidden,
								Message: "field is forbidden",
								Field:   "spec.field",
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			label: "status error with empty causes",
			err: &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Details: &metav1.StatusDetails{
						Causes: []metav1.StatusCause{},
					},
				},
			},
			expected: false,
		},
		{
			label: "status error with nil details",
			err: &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Details: nil,
				},
			},
			expected: false,
		},
	}

	for i, c := range cases {
		t.Logf("Case %d: %s\n", i+1, c.label)
		result := IsFieldImmutable(c.err)
		assert.Equal(t, c.expected, result)
	}
}
