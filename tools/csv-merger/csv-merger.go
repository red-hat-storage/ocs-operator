package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/blang/semver/v4"
	"github.com/ghodss/yaml"
	"github.com/operator-framework/api/pkg/lib/version"
	csvv1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	ocsversion "github.com/red-hat-storage/ocs-operator/v4/version"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
)

var (
	csvVersion         = flag.String("csv-version", "", "the unified CSV version")
	replacesCsvVersion = flag.String("replaces-csv-version", "", "the unified CSV version this new CSV will replace")
	skipRange          = flag.String("skip-range", "", "the CSV version skip range")
	rookCSVStr         = flag.String("rook-csv-filepath", "", "path to rook csv yaml file")
	noobaaCSVStr       = flag.String("noobaa-csv-filepath", "", "path to noobaa csv yaml file")
	ocsCSVStr          = flag.String("ocs-csv-filepath", "", "path to ocs csv yaml file")
	timestamp          = flag.String("timestamp", "false", "bool value to enable/disable timestamp changes in CSV")

	rookContainerImage       = flag.String("rook-image", "", "rook operator container image")
	cephContainerImage       = flag.String("ceph-image", "", "ceph daemon container image")
	rookCsiCephImage         = flag.String("rook-csi-ceph-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiRegistrarImage    = flag.String("rook-csi-registrar-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiResizerImage      = flag.String("rook-csi-resizer-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiProvisionerImage  = flag.String("rook-csi-provisioner-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiSnapshotterImage  = flag.String("rook-csi-snapshotter-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiAttacherImage     = flag.String("rook-csi-attacher-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	noobaaCoreContainerImage = flag.String("noobaa-core-image", "", "noobaa core container image")
	noobaaDBContainerImage   = flag.String("noobaa-db-image", "", "db container image for noobaa")
	ocsContainerImage        = flag.String("ocs-image", "", "ocs operator container image")
	ocsMetricsExporterImage  = flag.String("ocs-metrics-exporter-image", "", "ocs metrics exporter container image")
	uxBackendOauthImage      = flag.String("ux-backend-oauth-image", "", "ux backend oauth container image")
	ocsMustGatherImage       = flag.String("ocs-must-gather-image", "", "ocs-must-gather image")
	rookCsiAddonsImage       = flag.String("rook-csiaddons-image", "", "csi-addons container image")

	inputCrdsDir      = flag.String("crds-directory", "", "The directory containing all the crds to be included in the registry bundle")
	inputManifestsDir = flag.String("manifests-directory", "", "The directory containing the extra manifests to be included in the registry bundle")

	outputDir = flag.String("olm-bundle-directory", "", "The directory to output the unified CSV and CRDs to")

	// List of APIs which should be exposed in Console
	exposedAPIs = []string{
		"storageclusters.ocs.openshift.io",
		"cephblockpools.ceph.rook.io",
		"backingstores.noobaa.io",
		"bucketclasses.noobaa.io",
		"namespacestores.noobaa.io",
	}

	ocsNodeToleration = []corev1.Toleration{
		{
			Key:      "node.ocs.openshift.io/storage",
			Operator: corev1.TolerationOpEqual,
			Value:    "true",
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}

	// Regular expression used to decide if an image reference is using a digest instead of a
	// tag. For that kind of image references we want to use the `IfNotPresent` pull policy.
	digestImageReferenceRE = regexp.MustCompile("^.+@.+:.+$")
)

type templateData struct {
	RookOperatorImage      string
	RookOperatorCsvVersion string
	OcsOperatorCsvVersion  string
	OcsOperatorImage       string
}

func copyFile(src string, dst string) {
	srcFile, err := os.Open(src)
	if err != nil {
		panic(err)
	}
	defer srcFile.Close()

	outFile, err := os.Create(dst)
	if err != nil {
		panic(err)
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, srcFile)
	if err != nil {
		panic(err)
	}
}

func unmarshalCSV(filePath string) *csvv1.ClusterServiceVersion {
	data := templateData{
		RookOperatorImage:      *rookContainerImage,
		RookOperatorCsvVersion: *csvVersion,
		OcsOperatorCsvVersion:  *csvVersion,
		OcsOperatorImage:       *ocsContainerImage,
	}

	writer := strings.Builder{}

	fmt.Printf("reading in csv at %s\n", filePath)
	tmpl := template.Must(template.ParseFiles(filePath))
	err := tmpl.Execute(&writer, data)
	if err != nil {
		panic(err)
	}

	bytes := []byte(writer.String())

	csv := &csvv1.ClusterServiceVersion{}
	err = yaml.Unmarshal(bytes, csv)
	if err != nil {
		panic(err)
	}

	templateStrategySpec := &csv.Spec.InstallStrategy.StrategySpec

	// inject custom ENV VARS.
	if strings.Contains(csv.Name, "ocs") || strings.Contains(csv.Name, "ics") {
		vars := []corev1.EnvVar{
			{
				Name:  "ROOK_CEPH_IMAGE",
				Value: *rookContainerImage,
			},
			{
				Name:  "CEPH_IMAGE",
				Value: *cephContainerImage,
			},
			{
				Name:  "NOOBAA_CORE_IMAGE",
				Value: *noobaaCoreContainerImage,
			},
			{
				Name:  "NOOBAA_DB_IMAGE",
				Value: *noobaaDBContainerImage,
			},
			{
				Name:  "PROVIDER_API_SERVER_IMAGE",
				Value: *ocsContainerImage,
			},
			{
				Name:  "ONBOARDING_SECRET_GENERATOR_IMAGE",
				Value: *ocsContainerImage,
			},
			{
				Name: util.OperatorNamespaceEnvVar,
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					},
				},
			},
		}

		// append to env var list.
		templateStrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Env = append(templateStrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Env, vars...)

	} else if strings.Contains(csv.Name, "rook") || strings.Contains(csv.Name, "ceph") {
		vars := []corev1.EnvVar{
			{
				Name:  "ROOK_CURRENT_NAMESPACE_ONLY",
				Value: "true",
			},
			{
				Name:  "ROOK_ALLOW_MULTIPLE_FILESYSTEMS",
				Value: "false",
			},
			{
				Name:  "ROOK_LOG_LEVEL",
				Value: "INFO",
			},
			{
				Name:  "ROOK_CEPH_STATUS_CHECK_INTERVAL",
				Value: "60s",
			},
			{
				Name:  "ROOK_MON_HEALTHCHECK_INTERVAL",
				Value: "45s",
			},
			{
				Name:  "ROOK_MON_OUT_TIMEOUT",
				Value: "600s",
			},
			{
				Name:  "ROOK_DISCOVER_DEVICES_INTERVAL",
				Value: "60m",
			},
			{
				Name:  "ROOK_HOSTPATH_REQUIRES_PRIVILEGED",
				Value: "true",
			},
			{
				Name:  "ROOK_ENABLE_SELINUX_RELABELING",
				Value: "true",
			},
			{
				Name:  "ROOK_ENABLE_FSGROUP",
				Value: "true",
			},
			{
				Name:  "ROOK_ENABLE_FLEX_DRIVER",
				Value: "false",
			},
			{
				Name:  "ROOK_ENABLE_DISCOVERY_DAEMON",
				Value: "false",
			},
			{
				Name:  "ROOK_ENABLE_MACHINE_DISRUPTION_BUDGET",
				Value: "false",
			},
			{
				Name:  "ROOK_DISABLE_DEVICE_HOTPLUG",
				Value: "true",
			},
			{
				Name:  "ROOK_CSI_ALLOW_UNSUPPORTED_VERSION",
				Value: "true",
			},
			{
				Name:  "ROOK_DISABLE_ADMISSION_CONTROLLER",
				Value: "true",
			},
			{
				Name:  "ROOK_CSIADDONS_IMAGE",
				Value: *rookCsiAddonsImage,
			},
			{
				Name:  "CSI_ENABLE_METADATA",
				Value: "false",
			},
			{
				Name:  "CSI_PLUGIN_PRIORITY_CLASSNAME",
				Value: "system-node-critical",
			},
			{
				Name:  "CSI_PROVISIONER_PRIORITY_CLASSNAME",
				Value: "system-cluster-critical",
			},
			{
				Name: "CSI_CLUSTER_NAME",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "ocs-operator-config",
						},
						Key: "CSI_CLUSTER_NAME",
					},
				},
			},
			{
				Name: "CSI_ENABLE_READ_AFFINITY",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "ocs-operator-config",
						},
						Key: "CSI_ENABLE_READ_AFFINITY",
					},
				},
			},
			{
				Name: "CSI_CEPHFS_KERNEL_MOUNT_OPTIONS",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "ocs-operator-config",
						},
						Key: "CSI_CEPHFS_KERNEL_MOUNT_OPTIONS",
					},
				},
			},
			{
				Name: "CSI_ENABLE_TOPOLOGY",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "ocs-operator-config",
						},
						Key: "CSI_ENABLE_TOPOLOGY",
					},
				},
			},
			{
				Name: "CSI_TOPOLOGY_DOMAIN_LABELS",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "ocs-operator-config",
						},
						Key: "CSI_TOPOLOGY_DOMAIN_LABELS",
					},
				},
			},
			{
				Name: "ROOK_CSI_ENABLE_NFS",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "ocs-operator-config",
						},
						Key: "ROOK_CSI_ENABLE_NFS",
					},
				},
			},
			{
				Name: "CSI_PROVISIONER_TOLERATIONS",
				Value: `
- key: node.ocs.openshift.io/storage
  operator: Equal
  value: "true"
  effect: NoSchedule`,
			},
			{
				Name: "CSI_PLUGIN_TOLERATIONS",
				Value: `
- key: node.ocs.openshift.io/storage
  operator: Equal
  value: "true"
  effect: NoSchedule`,
			},
			{
				Name:  "CSI_LOG_LEVEL",
				Value: "5",
			},
			{
				Name:  "CSI_SIDECAR_LOG_LEVEL",
				Value: "1",
			},
			{
				Name:  "CSI_ENABLE_CSIADDONS",
				Value: "true",
			},
			{
				Name: "NODE_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "spec.nodeName",
					},
				},
			},
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.name",
					},
				},
			},
			{
				Name: "POD_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					},
				},
			},
			{
				Name:  "ROOK_OBC_WATCH_OPERATOR_NAMESPACE",
				Value: "true",
			},
		}

		if *rookCsiCephImage != "" {
			vars = append(vars, corev1.EnvVar{
				Name:  "ROOK_CSI_CEPH_IMAGE",
				Value: *rookCsiCephImage,
			})
		}
		if *rookCsiRegistrarImage != "" {
			vars = append(vars, corev1.EnvVar{
				Name:  "ROOK_CSI_REGISTRAR_IMAGE",
				Value: *rookCsiRegistrarImage,
			})
		}
		if *rookCsiResizerImage != "" {
			vars = append(vars, corev1.EnvVar{
				Name:  "ROOK_CSI_RESIZER_IMAGE",
				Value: *rookCsiResizerImage,
			})
		}
		if *rookCsiProvisionerImage != "" {
			vars = append(vars, corev1.EnvVar{
				Name:  "ROOK_CSI_PROVISIONER_IMAGE",
				Value: *rookCsiProvisionerImage,
			})
		}
		if *rookCsiSnapshotterImage != "" {
			vars = append(vars, corev1.EnvVar{
				Name:  "ROOK_CSI_SNAPSHOTTER_IMAGE",
				Value: *rookCsiSnapshotterImage,
			})
		}
		if *rookCsiAttacherImage != "" {
			vars = append(vars, corev1.EnvVar{
				Name:  "ROOK_CSI_ATTACHER_IMAGE",
				Value: *rookCsiAttacherImage,
			})
		}

		// override the rook env var list.
		templateStrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Env = vars
	}

	return csv
}

func marshallObject(obj interface{}, writer io.Writer, modifyUnstructuredFunc func(*unstructured.Unstructured) error) error {
	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return err
	}

	var r unstructured.Unstructured
	if err := json.Unmarshal(jsonBytes, &r.Object); err != nil {
		return err
	}

	// remove status and metadata.creationTimestamp
	unstructured.RemoveNestedField(r.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(r.Object, "template", "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(r.Object, "spec", "template", "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(r.Object, "status")

	deployments, exists, err := unstructured.NestedSlice(r.Object, "spec", "install", "spec", "deployments")
	if err != nil {
		return err
	}
	if exists {
		for _, obj := range deployments {
			deployment := obj.(map[string]interface{})
			unstructured.RemoveNestedField(deployment, "metadata", "creationTimestamp")
			unstructured.RemoveNestedField(deployment, "spec", "template", "metadata", "creationTimestamp")
			unstructured.RemoveNestedField(deployment, "status")
		}
		err := unstructured.SetNestedSlice(r.Object, deployments, "spec", "install", "spec", "deployments")
		if err != nil {
			return err
		}
	}

	if modifyUnstructuredFunc != nil {
		err := modifyUnstructuredFunc(&r)
		if err != nil {
			return err
		}
	}

	jsonBytes, err = json.Marshal(r.Object)
	if err != nil {
		return err
	}

	yamlBytes, err := yaml.JSONToYAML(jsonBytes)
	if err != nil {
		return err
	}

	// fix double quoted strings by removing unneeded single quotes...
	s := string(yamlBytes)
	s = strings.Replace(s, " '\"", " \"", -1)
	s = strings.Replace(s, "\"'\n", "\"\n", -1)

	yamlBytes = []byte(s)

	_, err = writer.Write([]byte("---\n"))
	if err != nil {
		return err
	}

	_, err = writer.Write(yamlBytes)
	if err != nil {
		return err
	}

	return nil
}

// Checks whether a string is contained within a slice
func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func generateUnifiedCSV() *csvv1.ClusterServiceVersion {
	ocsCSV := unmarshalCSV(*ocsCSVStr)
	rookCSV := unmarshalCSV(*rookCSVStr)
	noobaaCSV := unmarshalCSV(*noobaaCSVStr)

	mergeCsvs := []*csvv1.ClusterServiceVersion{
		rookCSV,
		noobaaCSV,
	}

	ocsCSV.Spec.CustomResourceDefinitions.Required = nil

	ocsCSV.Annotations["operators.operatorframework.io/internal-objects"] = ""

	templateStrategySpec := &ocsCSV.Spec.InstallStrategy.StrategySpec

	// Merge CSVs into Unified CSV
	for _, csv := range mergeCsvs {
		if csv == noobaaCSV {
			continue
		} else {
			strategySpec := csv.Spec.InstallStrategy.StrategySpec

			deploymentspecs := strategySpec.DeploymentSpecs
			clusterPermissions := strategySpec.ClusterPermissions
			permissions := strategySpec.Permissions

			templateStrategySpec.DeploymentSpecs = append(templateStrategySpec.DeploymentSpecs, deploymentspecs...)
			templateStrategySpec.ClusterPermissions = append(templateStrategySpec.ClusterPermissions, clusterPermissions...)
			templateStrategySpec.Permissions = append(templateStrategySpec.Permissions, permissions...)

			for _, definition := range csv.Spec.CustomResourceDefinitions.Owned {
				// do not add vr and vrc to csv, this will be owned by csi-addons now.
				if !(definition.Name == "volumereplications.replication.storage.openshift.io" ||
					definition.Name == "volumereplicationclasses.replication.storage.openshift.io") {
					ocsCSV.Spec.CustomResourceDefinitions.Owned = append(ocsCSV.Spec.CustomResourceDefinitions.Owned, definition)
				}
			}

		}
	}
	// whitelisting APIs
	for index, definition := range ocsCSV.Spec.CustomResourceDefinitions.Owned {
		if !contains(exposedAPIs, definition.Name) {
			if index == 0 {
				ocsCSV.Annotations["operators.operatorframework.io/internal-objects"] = "[" + "\"" + definition.Name + "\""
			} else if index == len(ocsCSV.Spec.CustomResourceDefinitions.Owned)-1 {
				ocsCSV.Annotations["operators.operatorframework.io/internal-objects"] = ocsCSV.Annotations["operators.operatorframework.io/internal-objects"] + "," + "\"" + definition.Name + "\"" + "]"
			} else {
				ocsCSV.Annotations["operators.operatorframework.io/internal-objects"] = ocsCSV.Annotations["operators.operatorframework.io/internal-objects"] + "," + "\"" + definition.Name + "\""
			}
		}
	}

	// Inject display name
	for i, definition := range ocsCSV.Spec.CustomResourceDefinitions.Owned {
		switch definition.Name {
		case "cephblockpools.ceph.rook.io":
			ocsCSV.Spec.CustomResourceDefinitions.Owned[i].DisplayName = "Block Pools"
		}

	}

	// Add metrics exporter deployment to CSV
	metricExporterStrategySpec := csvv1.StrategyDeploymentSpec{
		Name: "ocs-metrics-exporter",
		Spec: getMetricsExporterDeployment(),
	}
	templateStrategySpec.DeploymentSpecs = append(templateStrategySpec.DeploymentSpecs, metricExporterStrategySpec)
	uxBackendStrategySpec := csvv1.StrategyDeploymentSpec{
		Name: "ux-backend-server",
		Spec: getUXBackendServerDeployment(),
	}
	templateStrategySpec.DeploymentSpecs = append(templateStrategySpec.DeploymentSpecs, uxBackendStrategySpec)

	// Add tolerations to deployments
	for i := range templateStrategySpec.DeploymentSpecs {
		d := &templateStrategySpec.DeploymentSpecs[i]
		d.Spec.Template.Spec.Tolerations = ocsNodeToleration
	}

	templateStrategySpec.ClusterPermissions = append(templateStrategySpec.ClusterPermissions, csvv1.StrategyDeploymentPermissions{
		ServiceAccountName: "ocs-metrics-exporter",
		Rules: []rbac.PolicyRule{
			{
				APIGroups: []string{"monitoring.coreos.com"},
				Resources: []string{"*"},
				Verbs:     []string{"*"},
			},
		},
	})
	fmt.Println(templateStrategySpec.DeploymentSpecs)

	// Set correct csv versions and name
	semverVersion, err := semver.New(*csvVersion)
	if err != nil {
		panic(err)
	}
	v := version.OperatorVersion{Version: *semverVersion}
	ocsCSV.Spec.Version = v
	// base csv name is of the form ocs-operator.v0.0.0
	tempCSVName := strings.Split(ocsCSV.Name, ".v")[0] + ".v"
	ocsCSV.Name = tempCSVName + *csvVersion
	if *replacesCsvVersion != "" {
		ocsCSV.Spec.Replaces = tempCSVName + *replacesCsvVersion
	}

	ocsCSV.Labels = make(map[string]string)

	ocsCSV.Labels["operatorframework.io/arch.amd64"] = "supported"
	ocsCSV.Labels["operatorframework.io/arch.ppc64le"] = "supported"
	ocsCSV.Labels["operatorframework.io/arch.s390x"] = "supported"

	// Set Annotations
	if *skipRange != "" {
		ocsCSV.Annotations["olm.skipRange"] = *skipRange
	}

	// Feature gating for Console. The array values are unique identifiers provided by the console.
	// This can be used to enable/disable console support for any supported feature
	// Example: "features.ocs.openshift.io/enabled": `["external", "foo1", "foo2", ...]`
	ocsCSV.Annotations["features.ocs.openshift.io/enabled"] = `["kms", "arbiter", "flexible-scaling", "multus", "pool-management", "namespace-store", "mcg-standalone", "taint-nodes", "vault-sa-kms", "hpcs-kms"]`
	// Feature disablement flag for Console. The array values are unique identifiers provided by the console.
	// To be used to disable UI components. This is used to track migration of features.
	// Example: "features.ocs.openshift.io/disabled": `["external", "foo1", "foo2", ...]`
	ocsCSV.Annotations["features.ocs.openshift.io/disabled"] = `["ss-list", "install-wizard", "block-pool", "mcg-resource", "odf-dashboard", "common", "storage-provider", "storage-provisioner", "dashboard-resources", "csv-actions", "inventory-item", "alert-actions"]`
	// Used by UI to validate user uploaded metadata
	// Metadata is used to connect to an external cluster
	ocsCSV.Annotations["external.features.ocs.openshift.io/validation"] = `{"secrets":["rook-ceph-operator-creds", "rook-csi-rbd-node", "rook-csi-rbd-provisioner"], "configMaps": ["rook-ceph-mon-endpoints", "rook-ceph-mon"], "storageClasses": ["ceph-rbd"], "cephClusters": ["monitoring-endpoint"]}`
	// Injecting the RHCS exporter script present in Rook CSV
	ocsCSV.Annotations["external.features.ocs.openshift.io/export-script"] = rookCSV.GetAnnotations()["externalClusterScript"]
	if *timestamp == "true" {
		loc, err := time.LoadLocation("UTC")
		if err != nil {
			panic(err)
		}
		ocsCSV.Annotations["createdAt"] = time.Now().In(loc).Format("2006-01-02 15:04:05")
	}
	ocsCSV.Annotations["containerImage"] = "quay.io/ocs-dev/ocs-operator:" + ocsversion.Version
	ocsCSV.Annotations["capabilities"] = "Deep Insights"
	ocsCSV.Annotations["categories"] = "Storage"
	ocsCSV.Annotations["operators.operatorframework.io/operator-type"] = "non-standalone"
	ocsCSV.Annotations["operatorframework.io/suggested-namespace"] = "openshift-storage"
	ocsCSV.Annotations["operators.openshift.io/infrastructure-features"] = "[\"disconnected\"]"
	// Make Storage cluster the initialization resource
	ocsCSV.Annotations["operatorframework.io/initialization-resource"] = `
    {
        "apiVersion": "ocs.openshift.io/v1",
        "kind": "StorageCluster",
        "metadata": {
            "name": "example-storagecluster",
            "namespace": "openshift-storage"
        },
        "spec": {
            "manageNodes": false,
            "monPVCTemplate": {
                "spec": {
                    "accessModes": [
                        "ReadWriteOnce"
                    ],
                    "resources": {
                        "requests": {
                            "storage": "10Gi"
                        }
                    },
                    "storageClassName": "gp2-csi"
                }
            },
            "storageDeviceSets": [
                {
                    "count": 3,
                    "dataPVCTemplate": {
                        "spec": {
                            "accessModes": [
                                "ReadWriteOnce"
                            ],
                            "resources": {
                                "requests": {
                                    "storage": "1Ti"
                                }
                            },
                            "storageClassName": "gp2-csi",
                            "volumeMode": "Block"
                        }
                    },
                    "name": "example-deviceset",
                    "placement": {},
                    "portable": true,
                    "resources": {}
                }
            ]
        }
    }
	`
	// Used by UI to track platforms that support External Mode
	// None is reported as the Infrastructure type for some UPI/Baremetal (non-automated) environment
	ocsCSV.Annotations["external.features.ocs.openshift.io/supported-platforms"] = `["BareMetal", "None", "VSphere", "OpenStack", "oVirt"]`
	ocsCSV.Annotations["alm-examples"] = `
[
    {
        "apiVersion": "ocs.openshift.io/v1",
        "kind": "StorageCluster",
        "metadata": {
            "name": "example-storagecluster",
            "namespace": "openshift-storage"
        },
        "spec": {
            "manageNodes": false,
            "monPVCTemplate": {
                "spec": {
                    "accessModes": [
                        "ReadWriteOnce"
                    ],
                    "resources": {
                        "requests": {
                            "storage": "10Gi"
                        }
                    },
                    "storageClassName": "gp2-csi"
                }
            },
            "storageDeviceSets": [
                {
                    "count": 3,
                    "dataPVCTemplate": {
                        "spec": {
                            "accessModes": [
                                "ReadWriteOnce"
                            ],
                            "resources": {
                                "requests": {
                                    "storage": "1Ti"
                                }
                            },
                            "storageClassName": "gp2-csi",
                            "volumeMode": "Block"
                        }
                    },
                    "name": "example-deviceset",
                    "placement": {},
                    "portable": true,
                    "resources": {}
                }
            ]
        }
    }
]`

	// Ensure that all deployments that pull images by digest use the `IfNotPresent` image pull
	// policy. That is convenient because images that have already been pulled by digest can't
	// change, and using the `Always` pull policy introduces an additional round trip to the
	// registry server that isn't really necessary and can fail.
	setDeploymentsImagePullPolicy(ocsCSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs)

	// write unified CSV to out dir
	writer := strings.Builder{}
	err = marshallObject(ocsCSV, &writer, injectCSVRelatedImages)
	if err != nil {
		panic(err)
	}

	finalizedCsvFilename := strings.Split(tempCSVName, ".")[0] + ".clusterserviceversion.yaml"
	err = os.WriteFile(filepath.Join(*outputDir, finalizedCsvFilename), []byte(writer.String()), 0644)
	if err != nil {
		panic(err)
	}

	fmt.Printf("CSV written to %s\n", filepath.Join(*outputDir, finalizedCsvFilename))
	return ocsCSV
}

func setDeploymentsImagePullPolicy(deploymentSpecs []csvv1.StrategyDeploymentSpec) {
	for i := range deploymentSpecs {
		setDeploymentImagePullPolicy(&deploymentSpecs[i].Spec)
	}
}

func setDeploymentImagePullPolicy(deploymentSpec *appsv1.DeploymentSpec) {
	setContainersImagePullPolicy(deploymentSpec.Template.Spec.InitContainers)
	setContainersImagePullPolicy(deploymentSpec.Template.Spec.Containers)
}

func setContainersImagePullPolicy(containers []corev1.Container) {
	for i := range containers {
		setContainerImagePullPolicy(&containers[i])
	}
}

func setContainerImagePullPolicy(container *corev1.Container) {
	if digestImageReferenceRE.MatchString(container.Image) {
		container.ImagePullPolicy = corev1.PullIfNotPresent
	}
}

func injectCSVRelatedImages(r *unstructured.Unstructured) error {

	relatedImages := []interface{}{}

	if *rookContainerImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "rook-container",
			"image": *rookContainerImage,
		})
	}
	if *rookCsiCephImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "rook-csi",
			"image": *rookCsiCephImage,
		})
	}
	if *rookCsiRegistrarImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "rook-csi-registrar",
			"image": *rookCsiRegistrarImage,
		})
	}
	if *rookCsiResizerImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "rook-csi-resizer",
			"image": *rookCsiResizerImage,
		})
	}
	if *rookCsiProvisionerImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "rook-csi-provisioner",
			"image": *rookCsiProvisionerImage,
		})
	}
	if *rookCsiSnapshotterImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "rook-csi-snapshotter",
			"image": *rookCsiSnapshotterImage,
		})
	}
	if *rookCsiAttacherImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "rook-csi-attacher",
			"image": *rookCsiAttacherImage,
		})
	}
	if *cephContainerImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "ceph-container",
			"image": *cephContainerImage,
		})
	}
	if *rookCsiAddonsImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "csiaddons-sidecar",
			"image": *rookCsiAddonsImage,
		})
	}
	if *ocsMustGatherImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "ocs-must-gather",
			"image": *ocsMustGatherImage,
		})
	}
	if *ocsMetricsExporterImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "ocs-metrics-exporter",
			"image": *ocsMetricsExporterImage,
		})
	}
	if *uxBackendOauthImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "ux-backend-oauth-image",
			"image": *uxBackendOauthImage,
		})
	}
	return unstructured.SetNestedSlice(r.Object, relatedImages, "spec", "relatedImages")
}

func copyCrds(ocsCSV *csvv1.ClusterServiceVersion) {
	var crdFiles []string
	crdDirs := []string{"ocs", "rook", "noobaa"}

	for _, dir := range crdDirs {
		crdDir := filepath.Join(*inputCrdsDir, dir)
		files, err := os.ReadDir(crdDir)
		if err != nil {
			panic(err)
		}
		for _, file := range files {
			crdFiles = append(crdFiles, filepath.Join(crdDir, file.Name()))
		}
	}

	ownedCrds := map[string]*csvv1.CRDDescription{}
	for _, definition := range ocsCSV.Spec.CustomResourceDefinitions.Owned {
		ownedCrds[definition.Name] = &definition
	}

	for _, crdFile := range crdFiles {
		crdBytes, err := os.ReadFile(crdFile)
		if err != nil {
			panic(err)
		}

		fmt.Printf("reading CRD file %s\n", crdFile)
		// CRDs that refer to 'ObjectReference' fetches
		// spec description from K8s in incorrect YAML format.
		// This can not be prevented and documentation needs to
		// be updated in Kubernetes. Until then the work around is
		// to remove all occurrences of ' --- ' in description.
		// "---" is used as YAML separator and should not be replaced.
		// https://github.com/kubernetes/api/blob/master/core/v1/types.go#L5287-L5301
		crdBytes = []byte(strings.ReplaceAll(string(crdBytes), " --- ", " "))
		entries := strings.Split(string(crdBytes), "---")
		for _, entry := range entries {
			crd := extv1.CustomResourceDefinition{}
			err = yaml.Unmarshal([]byte(entry), &crd)
			if err != nil {
				panic(err)
			}
			if crd.Spec.Names.Singular == "" {
				// filters out empty entries caused by starting file with '---' separator
				continue
			}
			if ownedCrds[crd.Name] == nil {
				continue
			}
			outputFile := filepath.Join(*outputDir, fmt.Sprintf("%s.crd.yaml", crd.Spec.Names.Singular))
			writer := strings.Builder{}
			err := marshallObject(crd, &writer, nil)
			if err != nil {
				panic(err)
			}
			err = os.WriteFile(outputFile, []byte(writer.String()), 0644)
			if err != nil {
				panic(err)
			}
			fmt.Printf("CRD written to %s\n", outputFile)
		}
	}
}

func copyManifests() {
	manifests, err := os.ReadDir(*inputManifestsDir)
	if err != nil {
		panic(err)
	}

	for _, manifest := range manifests {
		// only copy yaml files
		if !strings.Contains(manifest.Name(), ".yaml") {
			continue
		}

		inputPath := filepath.Join(*inputManifestsDir, manifest.Name())
		outputPath := filepath.Join(*outputDir, manifest.Name())
		copyFile(inputPath, outputPath)
	}
}

func getMetricsExporterDeployment() appsv1.DeploymentSpec {
	replica := int32(1)
	privileged := false
	noRoot := true
	deployment := appsv1.DeploymentSpec{
		Replicas: &replica,
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app.kubernetes.io/component": "ocs-metrics-exporter",
				"app.kubernetes.io/name":      "ocs-metrics-exporter",
			},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"app.kubernetes.io/component": "ocs-metrics-exporter",
					"app.kubernetes.io/name":      "ocs-metrics-exporter",
					"app.kubernetes.io/version":   "0.0.1",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "ocs-metrics-exporter",
						SecurityContext: &corev1.SecurityContext{
							Privileged:             &privileged,
							RunAsNonRoot:           &noRoot,
							ReadOnlyRootFilesystem: ptr.To(true),
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "ceph-config",
								MountPath: "/etc/ceph",
							},
						},
						Image:   *ocsMetricsExporterImage,
						Command: []string{"/usr/local/bin/metrics-exporter"},
						Args:    []string{"--namespaces=$(WATCH_NAMESPACE)"},
						Env: []corev1.EnvVar{
							{
								Name: "WATCH_NAMESPACE",
								ValueFrom: &corev1.EnvVarSource{
									FieldRef: &corev1.ObjectFieldSelector{
										FieldPath: "metadata.namespace",
									},
								},
							},
						},
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: 8080,
							},
							{
								ContainerPort: 8081,
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "ceph-config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "ocs-metrics-exporter-ceph-conf",
								},
							},
						},
					},
				},
				ServiceAccountName: "ocs-metrics-exporter",
			},
		},
	}
	return deployment
}

func getUXBackendServerDeployment() appsv1.DeploymentSpec {
	replica := int32(1)
	ptrToTrue := true
	deployment := appsv1.DeploymentSpec{
		Replicas: &replica,
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app.kubernetes.io/component": "ux-backend-server",
				"app.kubernetes.io/name":      "ux-backend-server",
				"app":                         "ux-backend-server",
			},
		},
		Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"app.kubernetes.io/component": "ux-backend-server",
					"app.kubernetes.io/name":      "ux-backend-server",
					"app":                         "ux-backend-server",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "ux-backend-server",
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "onboarding-private-key",
								MountPath: "/etc/private-key",
							},
							{
								Name:      "ux-cert-secret",
								MountPath: "/etc/tls/private",
							},
						},
						Image:           *ocsContainerImage,
						ImagePullPolicy: "IfNotPresent",
						Command:         []string{"/usr/local/bin/ux-backend-server"},
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: 8080,
							},
						},
						Env: []corev1.EnvVar{
							{
								Name:  "ONBOARDING_TOKEN_LIFETIME",
								Value: os.Getenv("ONBOARDING_TOKEN_LIFETIME"),
							},
							{
								Name:  "UX_BACKEND_PORT",
								Value: os.Getenv("UX_BACKEND_PORT"),
							},
							{
								Name:  "TLS_ENABLED",
								Value: os.Getenv("TLS_ENABLED"),
							},
						},
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:           ptr.To(true),
							ReadOnlyRootFilesystem: ptr.To(true),
						},
					},
					{
						Name: "oauth-proxy",
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "ux-proxy-secret",
								MountPath: "/etc/proxy/secrets",
							},
							{
								Name:      "ux-cert-secret",
								MountPath: "/etc/tls/private",
							},
						},
						Image:           *uxBackendOauthImage,
						ImagePullPolicy: "IfNotPresent",
						Args: []string{"-provider=openshift",
							"-https-address=:8888",
							"-http-address=", "-email-domain=*",
							"-upstream=http://localhost:8080/",
							"-tls-cert=/etc/tls/private/tls.crt",
							"-tls-key=/etc/tls/private/tls.key",
							"-cookie-secret-file=/etc/proxy/secrets/session_secret",
							"-openshift-service-account=ux-backend-server",
							`-openshift-delegate-urls={"/":{"group":"ocs.openshift.io","resource":"storageclusters","namespace":"openshift-storage","verb":"create"}}`,
							"-openshift-ca=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"},
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: 8888,
							},
						},
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:           ptr.To(true),
							ReadOnlyRootFilesystem: ptr.To(true),
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "onboarding-private-key",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "onboarding-private-key",
								Optional:   &ptrToTrue,
							},
						},
					},
					{
						Name: "ux-proxy-secret",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "ux-backend-proxy",
							},
						},
					},
					{
						Name: "ux-cert-secret",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "ux-cert-secret",
							},
						},
					},
				},
				ServiceAccountName: "ux-backend-server",
			},
		},
	}
	return deployment
}

func main() {
	flag.Parse()

	if *csvVersion == "" {
		log.Fatal("--csv-version is required")
	} else if *rookCSVStr == "" {
		log.Fatal("--rook-csv-filepath is required")
	} else if *noobaaCSVStr == "" {
		log.Fatal("--noobaa-csv-filepath is required")
	} else if *ocsCSVStr == "" {
		log.Fatal("--ocs-csv-filepath is required")
	} else if *rookContainerImage == "" {
		log.Fatal("--rook-image is required")
	} else if *cephContainerImage == "" {
		log.Fatal("--ceph-image is required")
	} else if *noobaaCoreContainerImage == "" {
		log.Fatal("--noobaa-core-image is required")
	} else if *noobaaDBContainerImage == "" {
		log.Fatal("--noobaa-db-image is required")
	} else if *ocsContainerImage == "" {
		log.Fatal("--ocs-image is required")
	} else if *ocsMetricsExporterImage == "" {
		log.Fatal("--ocs-metrics-exporter-image is required")
	} else if *inputCrdsDir == "" {
		log.Fatal("--crds-directory is required")
	} else if *outputDir == "" {
		log.Fatal("--olm-bundle-directory is required")
	} else if *uxBackendOauthImage == "" {
		// this image can be used quay.io/openshift/origin-oauth-proxy:4.14
		log.Fatal("--ux-backend-oauth-image is required")
	}

	// start with a fresh output directory if it already exists
	os.RemoveAll(*outputDir)

	// create output directory
	err := os.MkdirAll(*outputDir, os.FileMode(0755))
	if err != nil {
		panic(err)
	}
	ocsCSV := generateUnifiedCSV()
	copyCrds(ocsCSV)
	copyManifests()
}
