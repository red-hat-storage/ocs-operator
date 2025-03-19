module github.com/red-hat-storage/ocs-operator/v4

go 1.23.0

toolchain go1.23.4

replace github.com/red-hat-storage/ocs-operator/api/v4 => ./api

replace github.com/red-hat-storage/ocs-operator/metrics/v4 => ./metrics

replace github.com/red-hat-storage/ocs-operator/services/provider/api/v4 => ./services/provider/api

require (
	github.com/RHsyseng/operator-utils v1.4.13
	github.com/blang/semver/v4 v4.0.0
	github.com/ceph/ceph-csi-operator/api v0.0.0-20250226123017-6e1c39c697cb
	github.com/ceph/ceph-csi/api v0.0.0-20250303134928-a80295bf8fd4
	github.com/csi-addons/kubernetes-csi-addons v0.12.0
	github.com/go-logr/logr v1.4.2
	github.com/google/go-cmp v0.7.0
	github.com/google/uuid v1.6.0
	github.com/imdario/mergo v0.3.16
	github.com/k8snetworkplumbingwg/network-attachment-definition-client v1.7.5
	github.com/kubernetes-csi/external-snapshotter/client/v8 v8.2.0
	github.com/noobaa/noobaa-operator/v5 v5.0.0-20250302162422-00b29b2310ad
	github.com/onsi/ginkgo/v2 v2.22.2
	github.com/onsi/gomega v1.36.2
	github.com/openshift/api v0.0.0-20250303104811-f587fb60f627
	github.com/openshift/build-machinery-go v0.0.0-20250211133638-a00a772ae1a2
	github.com/openshift/client-go v0.0.0-20250131180035-f7ec47e2d87a
	github.com/openshift/custom-resource-status v1.1.3-0.20220503160415-f2fdb4999d87
	github.com/operator-framework/api v0.29.0
	github.com/operator-framework/operator-lib v0.17.0
	github.com/operator-framework/operator-lifecycle-manager v0.31.0
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.80.1
	github.com/prometheus-operator/prometheus-operator/pkg/client v0.80.1
	github.com/red-hat-storage/ocs-client-operator/api v0.0.0-20250303120608-b25fe5ab0148
	github.com/red-hat-storage/ocs-operator/api/v4 v4.0.0-20250227172543-a22914aaf7d5
	github.com/red-hat-storage/ocs-operator/services/provider/api/v4 v4.0.0-20250227172543-a22914aaf7d5
	github.com/rook/rook/pkg/apis v0.0.0-20250303190112-9a5bf16247ce
	github.com/stretchr/testify v1.10.0
	go.uber.org/multierr v1.11.0
	golang.org/x/exp v0.0.0-20250228200357-dead58393ab7
	golang.org/x/net v0.35.0
	google.golang.org/grpc v1.70.0
	gopkg.in/ini.v1 v1.67.0
	gopkg.in/yaml.v3 v3.0.1
	gotest.tools/v3 v3.5.1
	k8s.io/api v0.32.2
	k8s.io/apiextensions-apiserver v0.32.1
	k8s.io/apimachinery v0.32.2
	k8s.io/client-go v0.32.1
	k8s.io/klog/v2 v2.130.1
	k8s.io/utils v0.0.0-20241210054802-24370beab758
	open-cluster-management.io/api v0.15.0
	sigs.k8s.io/controller-runtime v0.20.2
	sigs.k8s.io/yaml v1.4.0
)

require (
	github.com/asaskevich/govalidator v0.0.0-20230301143203-a9d515a09cc2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containernetworking/cni v1.2.3 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.12.1 // indirect
	github.com/evanphx/json-patch/v5 v5.9.11 // indirect
	github.com/fsnotify/fsnotify v1.8.0 // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/go-jose/go-jose/v4 v4.0.5 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/go-openapi/analysis v0.20.0 // indirect
	github.com/go-openapi/errors v0.21.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/loads v0.20.2 // indirect
	github.com/go-openapi/runtime v0.19.24 // indirect
	github.com/go-openapi/spec v0.20.6 // indirect
	github.com/go-openapi/strfmt v0.22.1 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-openapi/validate v0.20.2 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/gnostic-models v0.6.9 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20241210010833-40e02aabc2ad // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.7 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/go-secure-stdlib/parseutil v0.1.8 // indirect
	github.com/hashicorp/go-secure-stdlib/strutil v0.1.2 // indirect
	github.com/hashicorp/go-sockaddr v1.0.7 // indirect
	github.com/hashicorp/hcl v1.0.1-vault-7 // indirect
	github.com/hashicorp/vault/api v1.16.0 // indirect
	github.com/hashicorp/vault/api/auth/approle v0.8.0 // indirect
	github.com/hashicorp/vault/api/auth/kubernetes v0.8.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/kube-object-storage/lib-bucket-provisioner v0.0.0-20221122204822-d1a8c34382f1 // indirect
	github.com/libopenstorage/secrets v0.0.0-20240416031220-a17cf7f72c6c // indirect
	github.com/mailru/easyjson v0.9.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_golang v1.20.5 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.62.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/spf13/cobra v1.8.1 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.mongodb.org/mongo-driver v1.16.0 // indirect
	go.opentelemetry.io/otel v1.32.0 // indirect
	go.opentelemetry.io/otel/trace v1.32.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/crypto v0.33.0 // indirect
	golang.org/x/oauth2 v0.26.0 // indirect
	golang.org/x/sync v0.11.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	golang.org/x/term v0.29.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	golang.org/x/time v0.10.0 // indirect
	golang.org/x/tools v0.30.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20241202173237-19429a94021a // indirect
	google.golang.org/protobuf v1.36.5 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/apiserver v0.32.1 // indirect
	k8s.io/component-base v0.32.1 // indirect
	k8s.io/klog v1.0.0 // indirect
	k8s.io/kube-aggregator v0.32.0 // indirect
	k8s.io/kube-openapi v0.0.0-20241212222426-2c72e554b1e7 // indirect
	sigs.k8s.io/container-object-storage-interface-api v0.1.0 // indirect
	sigs.k8s.io/json v0.0.0-20241014173422-cfa47c3a1cc8 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.5.0 // indirect
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
