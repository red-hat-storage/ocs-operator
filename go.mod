module github.com/red-hat-storage/ocs-operator

go 1.16

require (
	cloud.google.com/go v0.81.0 // indirect
	github.com/RHsyseng/operator-utils v1.4.2
	github.com/blang/semver v3.5.1+incompatible
	github.com/blang/semver/v4 v4.0.0
	github.com/ceph/ceph-csi/api v0.0.0-20211006172825-b9beb2106b70
	github.com/ceph/go-ceph v0.12.0
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32
	github.com/go-logr/logr v0.4.0
	github.com/hashicorp/vault v1.8.5 // indirect
	github.com/imdario/mergo v0.3.12
	github.com/kube-object-storage/lib-bucket-provisioner v0.0.0-20220105185820-c1da9586e05b
	github.com/kubernetes-csi/external-snapshotter/client/v4 v4.1.0
	github.com/mitchellh/mapstructure v1.4.1 // indirect
	github.com/noobaa/noobaa-operator/v5 v5.0.0-20210912161037-7eb9969404e4
	github.com/oklog/run v1.1.0
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.19.0
	github.com/openshift/api v3.9.1-0.20190924102528-32369d4db2ad+incompatible
	github.com/openshift/build-machinery-go v0.0.0-20210702090207-9c7b89e8633a
	github.com/openshift/client-go v0.0.0-20210112165513-ebc401615f47
	github.com/openshift/custom-resource-status v0.0.0-20190812200727-7961da9a2eb7
	github.com/operator-framework/api v0.10.0
	github.com/operator-framework/operator-lib v0.6.0
	github.com/operator-framework/operator-lifecycle-manager v0.17.0
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.46.0
	github.com/prometheus/client_golang v1.11.0
	github.com/prometheus/client_model v0.2.0
	github.com/rook/rook v1.8.3
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.0
	go.mongodb.org/mongo-driver v1.5.1 // indirect
	go.uber.org/multierr v1.6.0
	golang.org/x/crypto v0.0.0-20211202192323-5770296d904e // indirect
	google.golang.org/grpc v1.36.1
	google.golang.org/protobuf v1.26.0
	gopkg.in/ini.v1 v1.57.0
	gotest.tools/v3 v3.0.3
	k8s.io/api v0.22.2
	k8s.io/apiextensions-apiserver v0.22.2
	k8s.io/apimachinery v0.22.2
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/klog v1.0.0
	k8s.io/klog/v2 v2.8.0
	sigs.k8s.io/controller-runtime v0.10.2
)

replace (
	github.com/kubernetes-incubator/external-storage => github.com/libopenstorage/external-storage v0.20.4-openstorage-rc3 // required by rook v1.7
	github.com/openshift/api => github.com/openshift/api v0.0.0-20201203102015-275406142edb // required for Quickstart CRD
	github.com/portworx/sched-ops => github.com/portworx/sched-ops v0.20.4-openstorage-rc3 // required by rook v1.7
	github.com/rook/rook => github.com/red-hat-storage/rook v1.1.0-beta.0.0.20220823164057-e89284d08921
	k8s.io/api => k8s.io/api v0.21.3
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.21.3
	k8s.io/apimachinery => k8s.io/apimachinery v0.21.3
	k8s.io/apiserver => k8s.io/apiserver v0.21.3
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.21.3
	k8s.io/client-go => k8s.io/client-go v0.21.3
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.21.3
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.21.3
	k8s.io/code-generator => k8s.io/code-generator v0.21.3
	k8s.io/component-base => k8s.io/component-base v0.21.3
	k8s.io/component-helpers => k8s.io/component-helpers v0.21.3
	k8s.io/controller-manager => k8s.io/controller-manager v0.21.3
	k8s.io/cri-api => k8s.io/cri-api v0.21.3
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.21.3
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.21.3
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.21.3
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.21.3
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.21.3
	k8s.io/kubectl => k8s.io/kubectl v0.21.3
	k8s.io/kubelet => k8s.io/kubelet v0.21.3
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.21.3
	k8s.io/metrics => k8s.io/metrics v0.21.3
	k8s.io/mount-utils => k8s.io/mount-utils v0.21.3
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.21.3
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.9.5
)

// This tag doesn't exist, but is imported by github.com/portworx/sched-ops.
exclude github.com/kubernetes-incubator/external-storage v0.20.4-openstorage-rc2
