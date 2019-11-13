module github.com/openshift/ocs-operator

go 1.12

require (
	cloud.google.com/go v0.40.0 // indirect
	github.com/RHsyseng/operator-utils v0.0.0-20190807020041-5344a0f594b8
	github.com/blang/semver v3.5.1+incompatible
	github.com/coreos/prometheus-operator v0.29.0
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.1.0
	github.com/go-openapi/spec v0.19.2
	github.com/noobaa/noobaa-operator/v2 v2.0.7
	github.com/onsi/ginkgo v1.10.1
	github.com/onsi/gomega v1.7.0
	github.com/openshift/api v3.9.1-0.20190904155310-a25bb2adc83e+incompatible
	github.com/openshift/client-go v0.0.0-20190813201236-5a5508328169
	github.com/openshift/custom-resource-status v0.0.0-20190812200727-7961da9a2eb7
	github.com/operator-framework/operator-lifecycle-manager v0.0.0-20190605231540-b8a4faf68e36
	github.com/operator-framework/operator-sdk v0.10.0
	github.com/rook/rook v1.1.3
	github.com/stretchr/testify v1.3.0
	google.golang.org/appengine v1.6.1 // indirect
	k8s.io/api v0.0.0-20191005115622-2e41325d9e4b
	k8s.io/apiextensions-apiserver v0.0.0-20190820104113-47893d27d7f7
	k8s.io/apimachinery v0.0.0-20191005115455-e71eb83a557c
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/kube-openapi v0.0.0-20190816220812-743ec37842bf
	sigs.k8s.io/controller-runtime v0.2.0
)

// Pinned to deps for controller-runtime v0.2.0
replace (
	k8s.io/api => k8s.io/api v0.0.0-20190409021203-6e4e0e4f393b
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20190409022649-727a075fdec8
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190404173353-6a84e37a896d
	k8s.io/client-go => k8s.io/client-go v0.0.0-20190409021438-1a26190bd76a
)
