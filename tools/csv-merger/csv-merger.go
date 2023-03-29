package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/blang/semver"
	yaml "github.com/ghodss/yaml"
	ocsversion "github.com/openshift/ocs-operator/version"
	"github.com/operator-framework/api/pkg/lib/version"
	csvv1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var (
	csvVersion         = flag.String("csv-version", "", "the unified CSV version")
	replacesCsvVersion = flag.String("replaces-csv-version", "", "the unified CSV version this new CSV will replace")
	skipRange          = flag.String("skip-range", "", "the CSV version skip range")
	rookCSVStr         = flag.String("rook-csv-filepath", "", "path to rook csv yaml file")
	noobaaCSVStr       = flag.String("noobaa-csv-filepath", "", "path to noobaa csv yaml file")
	ocsCSVStr          = flag.String("ocs-csv-filepath", "", "path to ocs csv yaml file")
	timestamp          = flag.String("timestamp", "false", "bool value to enable/disable timestamp changes in CSV")

	rookContainerImage               = flag.String("rook-image", "", "rook operator container image")
	cephContainerImage               = flag.String("ceph-image", "", "ceph daemon container image")
	rookCsiCephImage                 = flag.String("rook-csi-ceph-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiRegistrarImage            = flag.String("rook-csi-registrar-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiResizerImage              = flag.String("rook-csi-resizer-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiProvisionerImage          = flag.String("rook-csi-provisioner-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiSnapshotterImage          = flag.String("rook-csi-snapshotter-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiAttacherImage             = flag.String("rook-csi-attacher-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	noobaaContainerImage             = flag.String("noobaa-image", "", "noobaa operator container image")
	noobaaCoreContainerImage         = flag.String("noobaa-core-image", "", "noobaa core container image")
	noobaaDBContainerImage           = flag.String("noobaa-db-image", "", "db container image for noobaa")
	ocsContainerImage                = flag.String("ocs-image", "", "ocs operator container image")
	ocsMustGatherImage               = flag.String("ocs-must-gather-image", "", "ocs-must-gather image")
	volumeReplicationControllerImage = flag.String("vol-repl-image", "", "volume replication operator container image")

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
)

type templateData struct {
	RookOperatorImage        string
	RookOperatorCsvVersion   string
	NoobaaOperatorImage      string
	NoobaaOperatorCsvVersion string
	OcsOperatorCsvVersion    string
	OcsOperatorImage         string
}

func finalizedCsvFilename() string {
	return "ocs-operator.clusterserviceversion.yaml"
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
		RookOperatorImage:        *rookContainerImage,
		NoobaaOperatorImage:      *noobaaContainerImage,
		NoobaaOperatorCsvVersion: *csvVersion,
		RookOperatorCsvVersion:   *csvVersion,
		OcsOperatorCsvVersion:    *csvVersion,
		OcsOperatorImage:         *ocsContainerImage,
	}

	writer := strings.Builder{}

	fmt.Printf("reading in csv at %s\n", filePath)
	tmpl := template.Must(template.ParseFiles(filePath))
	err := tmpl.Execute(&writer, data)
	if err != nil {
		panic(err)
	}

	bytes := []byte(writer.String())

	csvStruct := &csvv1.ClusterServiceVersion{}
	err = yaml.Unmarshal(bytes, csvStruct)
	if err != nil {
		panic(err)
	}

	return csvStruct
}

func unmarshalStrategySpec(csv *csvv1.ClusterServiceVersion) *csvv1.StrategyDetailsDeployment {

	templateStrategySpec := &csv.Spec.InstallStrategy.StrategySpec

	// inject custom ENV VARS.
	if strings.Contains(csv.Name, "ocs") {
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
				Name:  "CSI_VOLUME_REPLICATION_IMAGE",
				Value: *volumeReplicationControllerImage,
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

	} else if strings.Contains(csv.Name, "noobaa") {

		vars := []corev1.EnvVar{
			{
				Name:  "NOOBAA_CORE_IMAGE",
				Value: *noobaaCoreContainerImage,
			},
			{
				Name:  "NOOBAA_DB_IMAGE",
				Value: *noobaaDBContainerImage,
			},
		}

		templateStrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Env =
			append(templateStrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Env, vars...)

		// TODO remove this if statement once issue
		// https://github.com/noobaa/noobaa-operator/issues/35 is resolved
		// this image should be set by the templator logic
		templateStrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Image = *noobaaContainerImage
	}

	return templateStrategySpec
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

	csvs := []string{
		*ocsCSVStr,
		*rookCSVStr,
		*noobaaCSVStr,
	}

	ocsCSV := unmarshalCSV(*ocsCSVStr)
	rookCSV := unmarshalCSV(*rookCSVStr)
	ocsCSV.Spec.CustomResourceDefinitions.Owned = nil
	ocsCSV.Spec.CustomResourceDefinitions.Required = nil

	ocsCSV.Spec.Icon = []csvv1.Icon{
		{
			Data:      "PHN2ZyBpZD0iTGF5ZXJfMSIgZGF0YS1uYW1lPSJMYXllciAxIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAxOTIgMTQ1Ij48ZGVmcz48c3R5bGU+LmNscy0xe2ZpbGw6I2UwMDt9PC9zdHlsZT48L2RlZnM+PHRpdGxlPlJlZEhhdC1Mb2dvLUhhdC1Db2xvcjwvdGl0bGU+PHBhdGggZD0iTTE1Ny43Nyw2Mi42MWExNCwxNCwwLDAsMSwuMzEsMy40MmMwLDE0Ljg4LTE4LjEsMTcuNDYtMzAuNjEsMTcuNDZDNzguODMsODMuNDksNDIuNTMsNTMuMjYsNDIuNTMsNDRhNi40Myw2LjQzLDAsMCwxLC4yMi0xLjk0bC0zLjY2LDkuMDZhMTguNDUsMTguNDUsMCwwLDAtMS41MSw3LjMzYzAsMTguMTEsNDEsNDUuNDgsODcuNzQsNDUuNDgsMjAuNjksMCwzNi40My03Ljc2LDM2LjQzLTIxLjc3LDAtMS4wOCwwLTEuOTQtMS43My0xMC4xM1oiLz48cGF0aCBjbGFzcz0iY2xzLTEiIGQ9Ik0xMjcuNDcsODMuNDljMTIuNTEsMCwzMC42MS0yLjU4LDMwLjYxLTE3LjQ2YTE0LDE0LDAsMCwwLS4zMS0zLjQybC03LjQ1LTMyLjM2Yy0xLjcyLTcuMTItMy4yMy0xMC4zNS0xNS43My0xNi42QzEyNC44OSw4LjY5LDEwMy43Ni41LDk3LjUxLjUsOTEuNjkuNSw5MCw4LDgzLjA2LDhjLTYuNjgsMC0xMS42NC01LjYtMTcuODktNS42LTYsMC05LjkxLDQuMDktMTIuOTMsMTIuNSwwLDAtOC40MSwyMy43Mi05LjQ5LDI3LjE2QTYuNDMsNi40MywwLDAsMCw0Mi41Myw0NGMwLDkuMjIsMzYuMywzOS40NSw4NC45NCwzOS40NU0xNjAsNzIuMDdjMS43Myw4LjE5LDEuNzMsOS4wNSwxLjczLDEwLjEzLDAsMTQtMTUuNzQsMjEuNzctMzYuNDMsMjEuNzdDNzguNTQsMTA0LDM3LjU4LDc2LjYsMzcuNTgsNTguNDlhMTguNDUsMTguNDUsMCwwLDEsMS41MS03LjMzQzIyLjI3LDUyLC41LDU1LC41LDc0LjIyYzAsMzEuNDgsNzQuNTksNzAuMjgsMTMzLjY1LDcwLjI4LDQ1LjI4LDAsNTYuNy0yMC40OCw1Ni43LTM2LjY1LDAtMTIuNzItMTEtMjcuMTYtMzAuODMtMzUuNzgiLz48L3N2Zz4=",
			MediaType: "image/svg+xml",
		},
	}

	ocsCSV.Annotations["operators.operatorframework.io/internal-objects"] = ""

	templateStrategySpec := &csvv1.StrategyDetailsDeployment{
		ClusterPermissions: []csvv1.StrategyDeploymentPermissions{},
		Permissions:        []csvv1.StrategyDeploymentPermissions{},
		DeploymentSpecs:    []csvv1.StrategyDeploymentSpec{},
	}

	// Merge CSVs into Unified CSV
	for _, csvStr := range csvs {
		if csvStr != "" {
			csvStruct := unmarshalCSV(csvStr)
			strategySpec := unmarshalStrategySpec(csvStruct)

			deploymentspecs := strategySpec.DeploymentSpecs
			clusterPermissions := strategySpec.ClusterPermissions
			permissions := strategySpec.Permissions

			templateStrategySpec.DeploymentSpecs = append(templateStrategySpec.DeploymentSpecs, deploymentspecs...)
			templateStrategySpec.ClusterPermissions = append(templateStrategySpec.ClusterPermissions, clusterPermissions...)
			templateStrategySpec.Permissions = append(templateStrategySpec.Permissions, permissions...)

			ocsCSV.Spec.CustomResourceDefinitions.Owned = append(ocsCSV.Spec.CustomResourceDefinitions.Owned, csvStruct.Spec.CustomResourceDefinitions.Owned...)

			for _, definition := range csvStruct.Spec.CustomResourceDefinitions.Required {
				// Move ob and obc to Owned list instead ot Required
				if definition.Name == "objectbucketclaims.objectbucket.io" || definition.Name == "objectbuckets.objectbucket.io" {
					ocsCSV.Spec.CustomResourceDefinitions.Owned = append(ocsCSV.Spec.CustomResourceDefinitions.Owned, definition)
				} else {
					ocsCSV.Spec.CustomResourceDefinitions.Required = append(ocsCSV.Spec.CustomResourceDefinitions.Owned, definition)
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

	// Inject display name and description for our OCS crds
	for i, definition := range ocsCSV.Spec.CustomResourceDefinitions.Owned {
		switch definition.Name {
		case "storageclusters.ocs.openshift.io":
			ocsCSV.Spec.CustomResourceDefinitions.Owned[i].DisplayName = "Storage Cluster"
			ocsCSV.Spec.CustomResourceDefinitions.Owned[i].Description = "Storage Cluster represents a OpenShift Container Storage Cluster including Ceph Cluster, NooBaa and all the storage and compute resources required."
			ocsCSV.Spec.CustomResourceDefinitions.Owned[i].Resources = []csvv1.APIResourceReference{
				{
					Name:    "cephclusters.ceph.rook.io",
					Kind:    "CephCluster",
					Version: "v1",
				},
				{
					Name:    "noobaas.noobaa.io",
					Kind:    "NooBaa",
					Version: "v1alpha1",
				},
			}
		case "ocsinitializations.ocs.openshift.io":
			ocsCSV.Spec.CustomResourceDefinitions.Owned[i].DisplayName = "OCS Initialization"
			ocsCSV.Spec.CustomResourceDefinitions.Owned[i].Description = "OCS Initialization represents the initial data to be created when the OCS operator is installed."
		case "storageclusterinitializations.ocs.openshift.io":
			ocsCSV.Spec.CustomResourceDefinitions.Owned[i].DisplayName = "StorageCluster Initialization"
			ocsCSV.Spec.CustomResourceDefinitions.Owned[i].Description = "StorageCluster Initialization represents a set of tasks the OCS operator wants to implement for every StorageCluster it encounters."
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

	ocsCSV.Spec.InstallStrategy.StrategySpec = *templateStrategySpec

	// Set correct csv versions and name
	semverVersion, err := semver.New(*csvVersion)
	if err != nil {
		panic(err)
	}
	v := version.OperatorVersion{Version: *semverVersion}
	ocsCSV.Spec.Version = v
	ocsCSV.Name = "ocs-operator.v" + *csvVersion
	if *replacesCsvVersion != "" {
		ocsCSV.Spec.Replaces = "ocs-operator.v" + *replacesCsvVersion
	}

	// Set api maturity
	ocsCSV.Spec.Maturity = "alpha"

	//set Install Modes
	ocsCSV.Spec.InstallModes = []csvv1.InstallMode{
		{
			Type:      csvv1.InstallModeTypeOwnNamespace,
			Supported: true,
		},
		{
			Type:      csvv1.InstallModeTypeSingleNamespace,
			Supported: true,
		},
		{
			Type:      csvv1.InstallModeTypeMultiNamespace,
			Supported: false,
		},
		{
			Type:      csvv1.InstallModeTypeAllNamespaces,
			Supported: false,
		},
	}

	// Set maintainers
	ocsCSV.Spec.Maintainers = []csvv1.Maintainer{
		{
			Name:  "Red Hat Support",
			Email: "ocs-support@redhat.com",
		},
	}

	// Set links
	ocsCSV.Spec.Links = []csvv1.AppLink{
		{
			Name: "Source Code",
			URL:  "https://github.com/openshift/ocs-operator",
		},
	}

	// Set Keywords
	ocsCSV.Spec.Keywords = []string{
		"storage",
		"rook",
		"ceph",
		"noobaa",
		"block storage",
		"shared filesystem",
		"object storage",
	}

	// Set Provider
	ocsCSV.Spec.Provider = csvv1.AppLink{
		Name: "Red Hat",
	}

	// Set Description
	ocsCSV.Spec.Description = `
**Red Hat OpenShift Container Storage** deploys three operators.

### OpenShift Container Storage operator

The OpenShift Container Storage operator is the primary operator for OpenShift Container Storage. It serves to facilitate the other operators in OpenShift Container Storage by performing administrative tasks outside their scope as well as watching and configuring their CustomResources.

### Rook

[Rook][1] deploys and manages Ceph on OpenShift, which provides block and file storage.

### NooBaa operator

The NooBaa operator deploys and manages the [NooBaa][2] Multi-Cloud Gateway on OpenShift, which provides object storage.

# Core Capabilities

* **Self-managing service:** No matter which supported storage technologies you choose, OpenShift Container Storage ensures that resources can be deployed and managed automatically.

* **Hyper-scale or hyper-converged:** With OpenShift Container Storage you can either build dedicated storage clusters or hyper-converged clusters where your apps run alongside storage.

* **File, Block, and Object provided by OpenShift Container Storage:** OpenShift Container Storage integrates Ceph with multiple storage presentations including object storage (compatible with S3), block storage, and POSIX-compliant shared file system.

* **Your data, protected:** OpenShift Container Storage efficiently distributes and replicates your data across your cluster to minimize the risk of data loss. With snapshots, cloning, and versioning, no more losing sleep over your data.

* **Elastic storage in your datacenter:** Scale is now possible in your datacenter. Get started with a few terabytes, and easily scale up.

* **Simplified data management:** Easily create hybrid and multi-cloud data storage for your workloads, using a single namespace.

[1]: https://rook.io
[2]: https://noobaa.io
`

	ocsCSV.Spec.DisplayName = "OpenShift Container Storage"

	ocsCSV.Labels = make(map[string]string)

	ocsCSV.Labels["operatorframework.io/arch.amd64"] = "supported"
	ocsCSV.Labels["operatorframework.io/arch.ppc64le"] = "supported"
	ocsCSV.Labels["operatorframework.io/arch.s390x"] = "supported"

	// Set Annotations
	if *skipRange != "" {
		ocsCSV.Annotations["olm.skipRange"] = *skipRange
	}

	// apiextensions/v1 is available only on Kubernetes 1.16+
	// This ensures that we don't try to install on lower versions ok K8s
	ocsCSV.Spec.MinKubeVersion = "1.16.0"

	// Feature gating for Console. The array values are unique identifiers provided by the console.
	// This can be used to enable/disable console support for any supported feature
	// Example: "features.ocs.openshift.io/enabled": `["external", "foo1", "foo2", ...]`
	ocsCSV.Annotations["features.ocs.openshift.io/enabled"] = `["kms", "arbiter", "flexible-scaling", "multus", "pool-management", "namespace-store"]`
	// Used by UI to validate user uploaded metdata
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
	ocsCSV.Annotations["repository"] = "https://github.com/openshift/ocs-operator"
	ocsCSV.Annotations["containerImage"] = "quay.io/ocs-dev/ocs-operator:" + ocsversion.Version
	ocsCSV.Annotations["description"] = "Red Hat OpenShift Container Storage provides hyperconverged storage for applications within an OpenShift cluster."
	ocsCSV.Annotations["support"] = "Red Hat"
	ocsCSV.Annotations["capabilities"] = "Deep Insights"
	ocsCSV.Annotations["categories"] = "Storage"
	ocsCSV.Annotations["operatorframework.io/suggested-namespace"] = "openshift-storage"
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
                    "storageClassName": "gp2"
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
                            "storageClassName": "gp2",
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
                    "storageClassName": "gp2"
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
                            "storageClassName": "gp2",
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

	// write unified CSV to out dir
	writer := strings.Builder{}
	err = marshallObject(ocsCSV, &writer, injectCSVRelatedImages)
	if err != nil {
		panic(err)
	}
	err = ioutil.WriteFile(filepath.Join(*outputDir, finalizedCsvFilename()), []byte(writer.String()), 0644)
	if err != nil {
		panic(err)
	}

	fmt.Printf("CSV written to %s\n", filepath.Join(*outputDir, finalizedCsvFilename()))
	return ocsCSV
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
	if *volumeReplicationControllerImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "volume-replication-operator",
			"image": *volumeReplicationControllerImage,
		})
	}
	if *noobaaContainerImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "noobaa-operator",
			"image": *noobaaContainerImage,
		})
	}
	if *noobaaCoreContainerImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "noobaa-core",
			"image": *noobaaCoreContainerImage,
		})
	}
	if *noobaaDBContainerImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "noobaa-db",
			"image": *noobaaDBContainerImage,
		})
	}
	if *ocsMustGatherImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "ocs-must-gather",
			"image": *ocsMustGatherImage,
		})
	}

	return unstructured.SetNestedSlice(r.Object, relatedImages, "spec", "relatedImages")
}

func copyCrds(ocsCSV *csvv1.ClusterServiceVersion) {
	var crdFiles []string
	crdDirs := []string{"ocs", "rook", "noobaa"}

	for _, dir := range crdDirs {
		crdDir := filepath.Join(*inputCrdsDir, dir)
		files, err := ioutil.ReadDir(crdDir)
		if err != nil {
			panic(err)
		}
		for _, file := range files {
			crdFiles = append(crdFiles, filepath.Join(crdDir, file.Name()))
		}
	}

	ownedCrds := map[string]*csvv1.CRDDescription{}
	requiredCrds := map[string]*csvv1.CRDDescription{}
	for _, definition := range ocsCSV.Spec.CustomResourceDefinitions.Owned {
		ownedCrds[definition.Name] = &definition
	}
	for _, definition := range ocsCSV.Spec.CustomResourceDefinitions.Required {
		requiredCrds[definition.Name] = &definition
	}

	for _, crdFile := range crdFiles {
		crdBytes, err := ioutil.ReadFile(crdFile)
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
			if requiredCrds[crd.Name] != nil {
				// filter out required entries
				continue
			}
			if ownedCrds[crd.Name] == nil {
				fmt.Printf("WARNING: CRD is not owned and not required %s\n", crd.Name)
			}
			outputFile := filepath.Join(*outputDir, fmt.Sprintf("%s.crd.yaml", crd.Spec.Names.Singular))
			writer := strings.Builder{}
			err := marshallObject(crd, &writer, nil)
			if err != nil {
				panic(err)
			}
			err = ioutil.WriteFile(outputFile, []byte(writer.String()), 0644)
			if err != nil {
				panic(err)
			}
			fmt.Printf("CRD written to %s\n", outputFile)
		}
	}
}

func copyManifests() {
	manifests, err := ioutil.ReadDir(*inputManifestsDir)
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
	runAsNonRoot := true
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
						Name:    "ocs-metrics-exporter",
						Image:   *ocsContainerImage,
						Command: []string{"/usr/local/bin/metrics-exporter"},
						Args:    []string{"--namespaces=openshift-storage"},
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: 8080,
							},
							{
								ContainerPort: 8081,
							},
						},
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot: &runAsNonRoot,
						},
					},
				},
				ServiceAccountName: "ocs-metrics-exporter",
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
	} else if *noobaaContainerImage == "" {
		log.Fatal("--noobaa-image is required")
	} else if *noobaaCoreContainerImage == "" {
		log.Fatal("--noobaa-core-image is required")
	} else if *noobaaDBContainerImage == "" {
		log.Fatal("--noobaa-db-image is required")
	} else if *ocsContainerImage == "" {
		log.Fatal("--ocs-image is required")
	} else if *inputCrdsDir == "" {
		log.Fatal("--crds-directory is required")
	} else if *outputDir == "" {
		log.Fatal("--olm-bundle-directory is required")
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
