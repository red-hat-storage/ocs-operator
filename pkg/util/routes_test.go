package util

import (
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestGetAdmittedRouteHost(t *testing.T) {
	tests := []struct {
		name          string
		route         *routev1.Route
		expectedHost  string
		expectedFound bool
	}{
		{
			name:          "no ingress",
			route:         &routev1.Route{},
			expectedHost:  "",
			expectedFound: false,
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
			expectedHost:  "",
			expectedFound: false,
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
			expectedHost:  "",
			expectedFound: false,
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
			expectedHost:  "",
			expectedFound: false,
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
			expectedHost:  "rgw.apps.example.com",
			expectedFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, found := GetAdmittedRouteHost(tt.route)
			assert.Equal(t, tt.expectedHost, host)
			assert.Equal(t, tt.expectedFound, found)
		})
	}
}
