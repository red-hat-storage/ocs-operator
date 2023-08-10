module github.com/red-hat-storage/ocs-operator/v4

go 1.20

require (
	github.com/RHsyseng/operator-utils v1.4.13
	github.com/blang/semver/v4 v4.0.0
	github.com/ceph/ceph-csi/api v0.0.0-20230713140649-15931b2e4f19
	github.com/ceph/go-ceph v0.22.0
	github.com/ghodss/yaml v1.0.1-0.20220118164431-d8423dcdf344
	github.com/go-logr/logr v1.2.4
	github.com/google/uuid v1.3.0
	github.com/imdario/mergo v0.3.16
	github.com/kube-object-storage/lib-bucket-provisioner v0.0.0-20221122204822-d1a8c34382f1
	github.com/kubernetes-csi/external-snapshotter/client/v6 v6.2.0
	github.com/noobaa/noobaa-operator/v5 v5.0.0-20230712090817-288892c02711
	github.com/oklog/run v1.1.0
	github.com/onsi/ginkgo/v2 v2.11.0
	github.com/onsi/gomega v1.27.9
	github.com/openshift/api v0.0.0-20230720094506-afcbe27aec7c
	github.com/openshift/build-machinery-go v0.0.0-20230306181456-d321ffa04533
	github.com/openshift/client-go v0.0.0-20230718165156-6014fb98e86a
	github.com/openshift/custom-resource-status v1.1.2
	github.com/operator-framework/api v0.17.7-0.20230626210316-aa3e49803e7b
	github.com/operator-framework/operator-lib v0.11.1-0.20230717184314-6efbe3a22f6f
	github.com/operator-framework/operator-lifecycle-manager v0.25.0
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.66.0
	github.com/prometheus-operator/prometheus-operator/pkg/client v0.66.0
	github.com/prometheus/client_golang v1.16.0
	github.com/prometheus/client_model v0.4.0
	github.com/rook/rook v1.12.1
	github.com/rook/rook/pkg/apis v0.0.0-20230803134004-99ba8cd6b647
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.8.4
	go.uber.org/multierr v1.11.0
	golang.org/x/net v0.14.0
	google.golang.org/grpc v1.56.2
	google.golang.org/protobuf v1.31.0
	gopkg.in/ini.v1 v1.67.0
	gotest.tools/v3 v3.5.0
	k8s.io/api v0.27.3
	k8s.io/apiextensions-apiserver v0.27.3
	k8s.io/apimachinery v0.27.3
	k8s.io/client-go v0.27.3
	k8s.io/klog/v2 v2.100.1
	k8s.io/utils v0.0.0-20230711102312-30195339c3c7
	open-cluster-management.io/api v0.11.0
	sigs.k8s.io/controller-runtime v0.15.0
)

require (
	github.com/armon/go-metrics v0.4.1 // indirect
	github.com/asaskevich/govalidator v0.0.0-20200907205600-7a23bdc65eef // indirect
	github.com/aws/aws-sdk-go v1.44.289 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v3 v3.2.2 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf // indirect
	github.com/coreos/pkg v0.0.0-20230601102743-20bbbf26f4d8 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.10.2 // indirect
	github.com/evanphx/json-patch v5.6.0+incompatible // indirect
	github.com/evanphx/json-patch/v5 v5.6.0 // indirect
	github.com/fatih/color v1.15.0 // indirect
	github.com/frankban/quicktest v1.14.3 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/go-jose/go-jose/v3 v3.0.0 // indirect
	github.com/go-logr/zapr v1.2.4 // indirect
	github.com/go-openapi/analysis v0.20.0 // indirect
	github.com/go-openapi/errors v0.20.1 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/loads v0.20.2 // indirect
	github.com/go-openapi/runtime v0.19.24 // indirect
	github.com/go-openapi/spec v0.20.6 // indirect
	github.com/go-openapi/strfmt v0.20.0 // indirect
	github.com/go-openapi/swag v0.22.4 // indirect
	github.com/go-openapi/validate v0.20.2 // indirect
	github.com/go-stack/stack v1.8.0 // indirect
	github.com/go-task/slim-sprig v0.0.0-20230315185526-52ccab3ef572 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/gnostic v0.6.9 // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20230510103437-eeec1cb781c3 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-hclog v1.5.0 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.4 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/go-secure-stdlib/parseutil v0.1.7 // indirect
	github.com/hashicorp/go-secure-stdlib/strutil v0.1.2 // indirect
	github.com/hashicorp/go-sockaddr v1.0.2 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/hashicorp/hcl v1.0.1-vault-5 // indirect
	github.com/hashicorp/vault v1.13.4 // indirect
	github.com/hashicorp/vault/api v1.9.2 // indirect
	github.com/hashicorp/vault/api/auth/approle v0.4.1 // indirect
	github.com/hashicorp/vault/sdk v0.9.1 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/libopenstorage/secrets v0.0.0-20230417161723-1022cc4d5aeb // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pierrec/lz4 v2.6.1+incompatible // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/common v0.44.0 // indirect
	github.com/prometheus/procfs v0.11.0 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	go.mongodb.org/mongo-driver v1.9.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/zap v1.24.0 // indirect
	golang.org/x/crypto v0.12.0 // indirect
	golang.org/x/oauth2 v0.9.0 // indirect
	golang.org/x/sys v0.11.0 // indirect
	golang.org/x/term v0.11.0 // indirect
	golang.org/x/text v0.12.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	golang.org/x/tools v0.9.3 // indirect
	gomodules.xyz/jsonpatch/v2 v2.3.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20230525154841-bd750badd5c6 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiserver v0.27.3 // indirect
	k8s.io/component-base v0.27.3 // indirect
	k8s.io/klog v1.0.0 // indirect
	k8s.io/kube-aggregator v0.26.1 // indirect
	k8s.io/kube-openapi v0.0.0-20230501164219-8b0f38b5fd1f // indirect
	sigs.k8s.io/container-object-storage-interface-api v0.1.0 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
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
