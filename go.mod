module github.com/openshift/ocs-operator

go 1.12

require (
	cloud.google.com/go v0.40.0 // indirect
	github.com/NYTimes/gziphandler v1.0.1 // indirect
	github.com/RHsyseng/operator-utils v0.0.0-20190807020041-5344a0f594b8
	github.com/coreos/go-semver v0.2.0
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.1.0
	github.com/go-openapi/spec v0.19.2
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/openshift/custom-resource-status v0.0.0-20190812200727-7961da9a2eb7
	github.com/operator-framework/operator-lifecycle-manager v0.0.0-20190128024246-5eb7ae5bdb7a
	github.com/operator-framework/operator-sdk v0.10.0
	github.com/prometheus/client_golang v0.9.4 // indirect
	github.com/rook/rook v1.0.1-0.20190829112330-82702f5cc5d6
	github.com/stretchr/testify v1.3.0
	go.uber.org/atomic v1.4.0 // indirect
	go.uber.org/zap v1.10.0 // indirect
	google.golang.org/appengine v1.6.1 // indirect
	k8s.io/api v0.0.0-20190820101039-d651a1528133
	k8s.io/apiextensions-apiserver v0.0.0-20190820104113-47893d27d7f7
	k8s.io/apimachinery v0.0.0-20190820100751-ac02f8882ef6
	k8s.io/client-go v11.0.0+incompatible
	k8s.io/kube-openapi v0.0.0-20190709113604-33be087ad058
	sigs.k8s.io/controller-runtime v0.1.12
)

// Pinned to kubernetes-1.13.4
replace (
	k8s.io/api => k8s.io/api v0.0.0-20190222213804-5cb15d344471
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20190228180357-d002e88f6236
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190221213512-86fb29eff628
	k8s.io/client-go => k8s.io/client-go v0.0.0-20190228174230-b40b2a5939e4
)
