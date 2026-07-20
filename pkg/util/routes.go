package util

import (
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
)

func GetAdmittedRouteHost(route *routev1.Route) (string, bool) {
	for i := range route.Status.Ingress {
		ing := &route.Status.Ingress[i]
		for c := range ing.Conditions {
			if ing.Conditions[c].Type == routev1.RouteAdmitted &&
				ing.Conditions[c].Status == corev1.ConditionTrue &&
				ing.Host != "" {
				return ing.Host, true
			}
		}
	}
	return "", false
}
