package templates

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
)

var KubeRBACProxyConfigMap = corev1.ConfigMap{
	Data: map[string]string{
		"config-file.json": (func() string {
			config := struct {
				Authorization struct {
					Static [2]struct {
						Path            string `json:"path"`
						ResourceRequest bool   `json:"resourceRequest"`
						Verb            string `json:"verb"`
					} `json:"static"`
				} `json:"authorization"`
			}{}

			item := &config.Authorization.Static[0]
			item.Verb = "get"
			item.Path = "/metrics"
			item.ResourceRequest = false

			item = &config.Authorization.Static[1]
			item.Verb = "get"
			item.Path = "/federate"
			item.ResourceRequest = false

			raw, _ := json.Marshal(config)
			return string(raw)
		})(),
	},
}
