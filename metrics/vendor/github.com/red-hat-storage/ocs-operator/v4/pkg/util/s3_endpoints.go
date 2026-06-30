package util

const (
	// provider-side ConfigMap that stores exported endpoint values
	OcsHubS3EndpointsConfigMapName = "ocs-s3-endpoints-list"
	// client-side ConfigMap name format
	ClientS3EndpointsConfigMapNameFormat = OcsHubS3EndpointsConfigMapName + "-%s"

	NoobaaS3EndpointKey = "noobaaS3"
)
