package util

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func TestGetGrpcStatus(t *testing.T) {
	t.Run("returns nil and false for nil error", func(t *testing.T) {
		st, ok := GetGrpcStatus(nil)
		assert.False(t, ok)
		assert.Nil(t, st)
	})

	t.Run("extracts status from direct gRPC error", func(t *testing.T) {
		grpcErr := status.Error(codes.Unavailable, "service unavailable")

		st, ok := GetGrpcStatus(grpcErr)
		assert.True(t, ok)
		assert.NotNil(t, st)
		assert.Equal(t, codes.Unavailable, st.Code())
		assert.Equal(t, "service unavailable", st.Message())
	})

	t.Run("extracts status from wrapped gRPC error", func(t *testing.T) {
		grpcErr := status.Error(codes.Unimplemented, "unknown service grpc.health.v1.Health")
		wrappedErr := fmt.Errorf("health check failed: %w", grpcErr)

		st, ok := GetGrpcStatus(wrappedErr)
		assert.True(t, ok)
		assert.NotNil(t, st)
		assert.Equal(t, codes.Unimplemented, st.Code())
		// Message will include wrapping context, just verify it contains the original
		assert.Contains(t, st.Message(), "unknown service grpc.health.v1.Health")
	})

	t.Run("extracts status from multiply wrapped gRPC error", func(t *testing.T) {
		grpcErr := status.Error(codes.DeadlineExceeded, "deadline exceeded")
		wrappedOnce := fmt.Errorf("health check failed: %w", grpcErr)
		wrappedTwice := fmt.Errorf("connection error: %w", wrappedOnce)

		st, ok := GetGrpcStatus(wrappedTwice)
		assert.True(t, ok)
		assert.NotNil(t, st)
		assert.Equal(t, codes.DeadlineExceeded, st.Code())
		// Message will include wrapping context, just verify it contains the original
		assert.Contains(t, st.Message(), "deadline exceeded")
	})

	t.Run("returns false for regular error", func(t *testing.T) {
		regularErr := fmt.Errorf("regular error")

		st, ok := GetGrpcStatus(regularErr)
		assert.False(t, ok)
		assert.Nil(t, st)
	})

	t.Run("returns false for error wrapped with %v instead of %w", func(t *testing.T) {
		grpcErr := status.Error(codes.Internal, "internal error")
		// Note: Using %v breaks the error chain
		brokenWrappedErr := fmt.Errorf("wrapped with percent-v: %v", grpcErr)

		st, ok := GetGrpcStatus(brokenWrappedErr)
		assert.False(t, ok)
		assert.Nil(t, st)
	})

	t.Run("handles all gRPC error codes", func(t *testing.T) {
		testCases := []struct {
			code    codes.Code
			message string
		}{
			{codes.Canceled, "canceled"},
			{codes.Unknown, "unknown"},
			{codes.InvalidArgument, "invalid argument"},
			{codes.DeadlineExceeded, "deadline exceeded"},
			{codes.NotFound, "not found"},
			{codes.AlreadyExists, "already exists"},
			{codes.PermissionDenied, "permission denied"},
			{codes.ResourceExhausted, "resource exhausted"},
			{codes.FailedPrecondition, "failed precondition"},
			{codes.Aborted, "aborted"},
			{codes.OutOfRange, "out of range"},
			{codes.Unimplemented, "unimplemented"},
			{codes.Internal, "internal"},
			{codes.Unavailable, "unavailable"},
			{codes.DataLoss, "data loss"},
			{codes.Unauthenticated, "unauthenticated"},
		}

		for _, tc := range testCases {
			t.Run(tc.code.String(), func(t *testing.T) {
				grpcErr := status.Error(tc.code, tc.message)
				wrappedErr := fmt.Errorf("wrapped: %w", grpcErr)

				st, ok := GetGrpcStatus(wrappedErr)
				assert.True(t, ok, "expected to extract status for code %s", tc.code)
				assert.NotNil(t, st)
				assert.Equal(t, tc.code, st.Code())
				// Message includes wrapping context, just verify it contains the original
				assert.Contains(t, st.Message(), tc.message)
			})
		}
	})
}
