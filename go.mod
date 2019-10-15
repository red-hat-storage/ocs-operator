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
	github.com/go-openapi/validate v0.18.0 // indirect
	github.com/googleapis/gnostic v0.2.0 // indirect
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/noobaa/noobaa-operator/v2 v2.0.2
	github.com/onsi/ginkgo v1.10.1
	github.com/onsi/gomega v1.7.0
	github.com/openshift/api v3.9.1-0.20190904155310-a25bb2adc83e+incompatible
	github.com/openshift/client-go v0.0.0-20190813201236-5a5508328169
	github.com/openshift/custom-resource-status v0.0.0-20190812200727-7961da9a2eb7
	github.com/operator-framework/operator-lifecycle-manager v0.0.0-20190605231540-b8a4faf68e36
	github.com/operator-framework/operator-sdk v0.10.0
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/rook/rook v1.1.2
	github.com/stretchr/testify v1.3.0
	go.uber.org/multierr v1.2.0 // indirect
	google.golang.org/appengine v1.6.1 // indirect
	k8s.io/api v0.0.0-20191005115622-2e41325d9e4b
	k8s.io/apiextensions-apiserver v0.0.0-20190820104113-47893d27d7f7
	k8s.io/apimachinery v0.0.0-20191005115455-e71eb83a557c
	k8s.io/client-go v11.0.0+incompatible
	k8s.io/kube-openapi v0.0.0-20190816220812-743ec37842bf
	sigs.k8s.io/controller-runtime v0.1.12
)

// Pinned to kubernetes-1.13.4
replace (
	k8s.io/api => k8s.io/api v0.0.0-20190222213804-5cb15d344471
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20190228180357-d002e88f6236
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190221213512-86fb29eff628
	k8s.io/client-go => k8s.io/client-go v0.0.0-20190228174230-b40b2a5939e4
)
