package util

import (
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestGetAdmittedRouteHost(t *testing.T) {
	tests := []struct {
		name     string
		route    *routev1.Route
		expected string
	}{
		{
			name:     "no ingress",
			route:    &routev1.Route{},
			expected: "",
		},
		{
			name: "ingress with no conditions",
			route: &routev1.Route{
				Status: routev1.RouteStatus{
					Ingress: []routev1.RouteIngress{
						{Host: "example.com"},
					},
				},
			},
			expected: "",
		},
		{
			name: "ingress not admitted",
			route: &routev1.Route{
				Status: routev1.RouteStatus{
					Ingress: []routev1.RouteIngress{
						{
							Host: "example.com",
							Conditions: []routev1.RouteIngressCondition{
								{Type: routev1.RouteAdmitted, Status: corev1.ConditionFalse},
							},
						},
					},
				},
			},
			expected: "",
		},
		{
			name: "admitted with empty host",
			route: &routev1.Route{
				Status: routev1.RouteStatus{
					Ingress: []routev1.RouteIngress{
						{
							Host: "",
							Conditions: []routev1.RouteIngressCondition{
								{Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue},
							},
						},
					},
				},
			},
			expected: "",
		},
		{
			name: "admitted with host",
			route: &routev1.Route{
				Status: routev1.RouteStatus{
					Ingress: []routev1.RouteIngress{
						{
							Host: "rgw.apps.example.com",
							Conditions: []routev1.RouteIngressCondition{
								{Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue},
							},
						},
					},
				},
			},
			expected: "rgw.apps.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, GetAdmittedRouteHost(tt.route))
		})
	}
}
