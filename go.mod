module github.com/red-hat-storage/ocs-operator/v4

go 1.22.0

replace github.com/red-hat-storage/ocs-operator/api/v4 => ./api

replace github.com/red-hat-storage/ocs-operator/metrics/v4 => ./metrics

require (
	github.com/RHsyseng/operator-utils v1.4.13
	github.com/blang/semver/v4 v4.0.0
	github.com/ceph/ceph-csi-operator/api v0.0.0-20240812072523-4d50cf3a32a0
	github.com/ceph/ceph-csi/api v0.0.0-20240731141259-e3697f4d3e49
	github.com/go-logr/logr v1.4.2
	github.com/google/go-cmp v0.6.0
	github.com/google/uuid v1.6.0
	github.com/imdario/mergo v0.3.16
	github.com/k8snetworkplumbingwg/network-attachment-definition-client v1.7.0
	github.com/kubernetes-csi/external-snapshotter/client/v8 v8.0.0
	github.com/noobaa/noobaa-operator/v5 v5.0.0-20240729130336-d3d80339937e
	github.com/onsi/ginkgo/v2 v2.20.0
	github.com/onsi/gomega v1.34.1
	github.com/openshift/api v0.0.0-20240731234847-c299a673ec72
	github.com/openshift/build-machinery-go v0.0.0-20240419090851-af9c868bcf52
	github.com/openshift/client-go v0.0.0-20240528061634-b054aa794d87
	github.com/openshift/custom-resource-status v1.1.3-0.20220503160415-f2fdb4999d87
	github.com/operator-framework/api v0.26.0
	github.com/operator-framework/operator-lib v0.14.0
	github.com/operator-framework/operator-lifecycle-manager v0.28.0
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.75.2
	github.com/prometheus-operator/prometheus-operator/pkg/client v0.75.2
	github.com/red-hat-storage/ocs-client-operator v0.0.0-20240731100835-31192d2c370e
	github.com/red-hat-storage/ocs-operator/api/v4 v4.0.0-20240731064750-930a78b89d84
	github.com/rook/rook/pkg/apis v0.0.0-20240819063246-893ea7f6d6a1
	github.com/stretchr/testify v1.9.0
	go.uber.org/multierr v1.11.0
	golang.org/x/net v0.28.0
	google.golang.org/grpc v1.63.3
	google.golang.org/protobuf v1.34.2
	gopkg.in/ini.v1 v1.67.0
	gopkg.in/yaml.v2 v2.4.0
	gotest.tools/v3 v3.5.1
	k8s.io/api v0.30.3
	k8s.io/apiextensions-apiserver v0.30.3
	k8s.io/apimachinery v0.30.3
	k8s.io/client-go v0.30.3
	k8s.io/klog/v2 v2.130.1
	k8s.io/utils v0.0.0-20240711033017-18e509b52bc8
	open-cluster-management.io/api v0.13.0
	sigs.k8s.io/controller-runtime v0.18.4
	sigs.k8s.io/yaml v1.4.0
)

require (
	github.com/Masterminds/semver/v3 v3.2.1 // indirect
	github.com/asaskevich/govalidator v0.0.0-20230301143203-a9d515a09cc2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v3 v3.2.2 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containernetworking/cni v1.2.0-rc1 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.12.1 // indirect
	github.com/evanphx/json-patch v5.9.0+incompatible // indirect
	github.com/evanphx/json-patch/v5 v5.9.0 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/go-jose/go-jose/v4 v4.0.1 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/go-openapi/analysis v0.20.0 // indirect
	github.com/go-openapi/errors v0.20.1 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/loads v0.20.2 // indirect
	github.com/go-openapi/runtime v0.19.24 // indirect
	github.com/go-openapi/spec v0.20.6 // indirect
	github.com/go-openapi/strfmt v0.20.1 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-openapi/validate v0.20.2 // indirect
	github.com/go-stack/stack v1.8.0 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20240727154555-813a5fbdbec8 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.7 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/go-secure-stdlib/parseutil v0.1.8 // indirect
	github.com/hashicorp/go-secure-stdlib/strutil v0.1.2 // indirect
	github.com/hashicorp/go-sockaddr v1.0.6 // indirect
	github.com/hashicorp/hcl v1.0.1-vault-5 // indirect
	github.com/hashicorp/vault/api v1.14.0 // indirect
	github.com/hashicorp/vault/api/auth/approle v0.6.0 // indirect
	github.com/hashicorp/vault/api/auth/kubernetes v0.6.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kube-object-storage/lib-bucket-provisioner v0.0.0-20221122204822-d1a8c34382f1 // indirect
	github.com/libopenstorage/secrets v0.0.0-20240416031220-a17cf7f72c6c // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_golang v1.19.0 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.48.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	go.mongodb.org/mongo-driver v1.9.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/crypto v0.26.0 // indirect
	golang.org/x/exp v0.0.0-20240719175910-8a7402abbf56 // indirect
	golang.org/x/oauth2 v0.21.0 // indirect
	golang.org/x/sys v0.23.0 // indirect
	golang.org/x/term v0.23.0 // indirect
	golang.org/x/text v0.17.0 // indirect
	golang.org/x/time v0.5.0 // indirect
	golang.org/x/tools v0.24.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240528184218-531527333157 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiserver v0.30.3 // indirect
	k8s.io/klog v1.0.0 // indirect
	k8s.io/kube-aggregator v0.29.3 // indirect
	k8s.io/kube-openapi v0.0.0-20240620174524-b456828f718b // indirect
	sigs.k8s.io/container-object-storage-interface-api v0.1.0 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
)

replace github.com/portworx/sched-ops => github.com/portworx/sched-ops v0.20.4-openstorage-rc3 // required by rook

exclude (
	// This tag doesn't exist, but is imported by github.com/portworx/sched-ops.
	github.com/kubernetes-incubator/external-storage v0.20.4-openstorage-rc2
	// Exclude the v3.9 tags, because these are very old
	// but are picked when updating dependencies.
	github.com/openshift/api v3.9.0+incompatible
	github.com/openshift/api v3.9.1-0.20190924102528-32369d4db2ad+incompatible

	// Exclude pre-go-mod kubernetes tags, because they are older
	// than v0.x releases but are picked when updating dependencies.
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/client-go v12.0.0+incompatible
)
