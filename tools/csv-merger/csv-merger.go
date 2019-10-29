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

	"github.com/blang/semver"
	yaml "github.com/ghodss/yaml"
	csvv1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/lib/version"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	extv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type csvClusterPermissions struct {
	ServiceAccountName string              `json:"serviceAccountName"`
	Rules              []rbacv1.PolicyRule `json:"rules"`
}

type csvPermissions struct {
	ServiceAccountName string              `json:"serviceAccountName"`
	Rules              []rbacv1.PolicyRule `json:"rules"`
}

type csvDeployments struct {
	Name string                `json:"name"`
	Spec appsv1.DeploymentSpec `json:"spec,omitempty"`
}

type csvStrategySpec struct {
	ClusterPermissions []csvClusterPermissions `json:"clusterPermissions"`
	Permissions        []csvPermissions        `json:"permissions"`
	Deployments        []csvDeployments        `json:"deployments"`
}

var (
	csvVersion         = flag.String("csv-version", "", "the unified CSV version")
	replacesCsvVersion = flag.String("replaces-csv-version", "", "the unified CSV version this new CSV will replace")
	skipRange          = flag.String("skip-range", "", "the CSV version skip range")
	rookCSVStr         = flag.String("rook-csv-filepath", "", "path to rook csv yaml file")
	noobaaCSVStr       = flag.String("noobaa-csv-filepath", "", "path to noobaa csv yaml file")
	ocsCSVStr          = flag.String("ocs-csv-filepath", "", "path to ocs csv yaml file")

	rookContainerImage       = flag.String("rook-image", "", "rook operator container image")
	cephContainerImage       = flag.String("ceph-image", "", "ceph daemon container image")
	rookCsiCephImage         = flag.String("rook-csi-ceph-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiRegistrarImage    = flag.String("rook-csi-registrar-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiProvisionerImage  = flag.String("rook-csi-provisioner-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiSnapshotterImage  = flag.String("rook-csi-snapshotter-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiAttacherImage     = flag.String("rook-csi-attacher-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	noobaaContainerImage     = flag.String("noobaa-image", "", "noobaa operator container image")
	noobaaCoreContainerImage = flag.String("noobaa-core-image", "", "noobaa core container image")
	noobaaDBContainerImage   = flag.String("noobaa-db-image", "", "db container image for noobaa")
	ocsContainerImage        = flag.String("ocs-image", "", "ocs operator container image")

	inputCrdsDir      = flag.String("crds-directory", "", "The directory containing all the crds to be included in the registry bundle")
	inputManifestsDir = flag.String("manifests-directory", "", "The directory containing the extra manifests to be included in the registry bundle")

	outputDir = flag.String("olm-bundle-directory", "", "The directory to output the unified CSV and CRDs to")
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
	return "ocs-operator.v" + *csvVersion + ".clusterserviceversion.yaml"
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

func unmarshalStrategySpec(csv *csvv1.ClusterServiceVersion) *csvStrategySpec {

	templateStrategySpec := &csvStrategySpec{}
	err := json.Unmarshal(csv.Spec.InstallStrategy.StrategySpecRaw, templateStrategySpec)
	if err != nil {
		panic(err)
	}

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
			{
				Name:  "MON_COUNT_OVERRIDE",
				Value: "3",
			},
		}

		// append to env var list.
		templateStrategySpec.Deployments[0].Spec.Template.Spec.Containers[0].Env = append(templateStrategySpec.Deployments[0].Spec.Template.Spec.Containers[0].Env, vars...)

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
		templateStrategySpec.Deployments[0].Spec.Template.Spec.Containers[0].Env = vars
	} else if strings.Contains(csv.Name, "noobaa") {
		// TODO remove this if statement once issue
		// https://github.com/noobaa/noobaa-operator/issues/35 is resolved
		// this image should be set by the templator logic
		templateStrategySpec.Deployments[0].Spec.Template.Spec.Containers[0].Image = *noobaaContainerImage
	}

	return templateStrategySpec
}

func marshallObject(obj interface{}, writer io.Writer) error {
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
	if exists {
		for _, obj := range deployments {
			deployment := obj.(map[string]interface{})
			unstructured.RemoveNestedField(deployment, "metadata", "creationTimestamp")
			unstructured.RemoveNestedField(deployment, "spec", "template", "metadata", "creationTimestamp")
			unstructured.RemoveNestedField(deployment, "status")
		}
		unstructured.SetNestedSlice(r.Object, deployments, "spec", "install", "spec", "deployments")
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

func generateUnifiedCSV() {

	csvs := []string{
		*rookCSVStr,
		*noobaaCSVStr,
		*ocsCSVStr,
	}

	ocsCSV := unmarshalCSV(*ocsCSVStr)
	ocsCSV.Spec.CustomResourceDefinitions.Owned = nil

	templateStrategySpec := csvStrategySpec{
		Deployments:        []csvDeployments{},
		Permissions:        []csvPermissions{},
		ClusterPermissions: []csvClusterPermissions{},
	}

	// Merge CSVs into Unified CSV
	for _, csvStr := range csvs {
		if csvStr != "" {
			csvStruct := unmarshalCSV(csvStr)
			strategySpec := unmarshalStrategySpec(csvStruct)

			deployments := strategySpec.Deployments
			clusterPermissions := strategySpec.ClusterPermissions
			permissions := strategySpec.Permissions

			templateStrategySpec.Deployments = append(templateStrategySpec.Deployments, deployments...)
			templateStrategySpec.ClusterPermissions = append(templateStrategySpec.ClusterPermissions, clusterPermissions...)
			templateStrategySpec.Permissions = append(templateStrategySpec.Permissions, permissions...)

			ocsCSV.Spec.CustomResourceDefinitions.Owned = append(ocsCSV.Spec.CustomResourceDefinitions.Owned, csvStruct.Spec.CustomResourceDefinitions.Owned...)
			ocsCSV.Spec.CustomResourceDefinitions.Required = append(ocsCSV.Spec.CustomResourceDefinitions.Required, csvStruct.Spec.CustomResourceDefinitions.Required...)
		}
	}

	// Inject deplay names and descriptions for our OCS crds
	for i, definition := range ocsCSV.Spec.CustomResourceDefinitions.Owned {
		switch definition.Name {
		case "storageclusters.ocs.openshift.io":
			ocsCSV.Spec.CustomResourceDefinitions.Owned[i].DisplayName = "Storage Cluster"
			ocsCSV.Spec.CustomResourceDefinitions.Owned[i].Description = "Storage Cluster represents a Rook Ceph storage cluster including all the storage and compute resources required."
			ocsCSV.Spec.CustomResourceDefinitions.Owned[i].Resources = []csvv1.APIResourceReference{
				csvv1.APIResourceReference{
					Name:    "cephclusters.ceph.rook.io",
					Kind:    "CephCluster",
					Version: "v1",
				},
				csvv1.APIResourceReference{
					Name:    "noobaas.noobaa.io",
					Kind:    "NooBaa",
					Version: "v1alpha1",
				},
			}
		case "ocsinitializations.ocs.openshift.io":
			ocsCSV.Spec.CustomResourceDefinitions.Owned[i].DisplayName = "OCS Initialization"
			ocsCSV.Spec.CustomResourceDefinitions.Owned[i].Description = "OCS Initialization represents the initial data to be created when the OCS operator is installed"
		case "storageclusterinitializations.ocs.openshift.io":
			ocsCSV.Spec.CustomResourceDefinitions.Owned[i].DisplayName = "StorageCluster Initialization"
			ocsCSV.Spec.CustomResourceDefinitions.Owned[i].Description = "StorageCluster Initialization represents a set of tasks the OCS operator wants to implement for every StorageCluster it encounters."
		}
	}

	// Add crd Requirements.
	ocsCSV.Spec.CustomResourceDefinitions.Required = append(ocsCSV.Spec.CustomResourceDefinitions.Required, csvv1.CRDDescription{
		Name:        "localvolumes.local.storage.openshift.io",
		Version:     "v1",
		Kind:        "LocalVolume",
		DisplayName: "Local Volume",
		Description: "Local Storage Operator",
	})

	// Add tolerations to deployments
	for i := range templateStrategySpec.Deployments {
		d := &templateStrategySpec.Deployments[i]
		d.Spec.Template.Spec.Tolerations = []corev1.Toleration{
			{
				Key:      "node.ocs.openshift.io/storage",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			},
		}
	}
	fmt.Println(templateStrategySpec.Deployments)

	// Re-serialize deployments and permissions into csv strategy.
	updatedStrat, err := json.Marshal(templateStrategySpec)
	if err != nil {
		panic(err)
	}
	ocsCSV.Spec.InstallStrategy.StrategySpecRaw = updatedStrat

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
		csvv1.InstallMode{
			Type:      csvv1.InstallModeTypeOwnNamespace,
			Supported: true,
		},
		csvv1.InstallMode{
			Type:      csvv1.InstallModeTypeSingleNamespace,
			Supported: true,
		},
		csvv1.InstallMode{
			Type:      csvv1.InstallModeTypeMultiNamespace,
			Supported: false,
		},
		csvv1.InstallMode{
			Type:      csvv1.InstallModeTypeAllNamespaces,
			Supported: false,
		},
	}

	// Set maintainers
	ocsCSV.Spec.Maintainers = []csvv1.Maintainer{
		{
			Name:  "Jose Rivera",
			Email: "jarrpa@redhat.com",
		},
		{
			Name:  "Kaushal M",
			Email: "kaushal@redhat.com",
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
Red Hat Openshift Container Storage (OCS) provides hyperconverged storage for applications within an Openshift cluster.

The OCS operator is the primary operator for Red Hat OpenShift Container Storage (OCS). It serves to facilitate the other operators in OCS by performing administrative tasks outside their scope as well as watching and configuring their CustomResources.`

	ocsCSV.Spec.DisplayName = "Openshift Container Storage Operator"

	// Set Annotations
	if *skipRange != "" {
		ocsCSV.Annotations["olm.skipRange"] = *skipRange
	}
	ocsCSV.Annotations["capabilities"] = "Full Lifecycle"
	ocsCSV.Annotations["categories"] = "Storage"
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
    },
    {
        "apiVersion": "ocs.openshift.io/v1",
        "kind": "OCSInitialization",
        "metadata": {
            "name": "example-ocsinitialization"
        },
        "spec": {}
    },
    {
        "apiVersion": "ocs.openshift.io/v1",
        "kind": "StorageClusterInitialization",
        "metadata": {
            "name": "example-storageclusterinitialization"
        },
        "spec": {}
    }
]`

	// write unified CSV to out dir
	writer := strings.Builder{}
	marshallObject(ocsCSV, &writer)
	err = ioutil.WriteFile(filepath.Join(*outputDir, finalizedCsvFilename()), []byte(writer.String()), 0644)
	if err != nil {
		panic(err)
	}

	fmt.Printf("CSV written to %s\n", filepath.Join(*outputDir, finalizedCsvFilename()))
}

func copyCrds() {
	crdFiles, err := ioutil.ReadDir(*inputCrdsDir)
	if err != nil {
		panic(err)
	}

	for _, crdFile := range crdFiles {
		// only copy crd manifests, this will ignore cr manifests
		if !strings.Contains(crdFile.Name(), "crd.yaml") {
			continue
		}

		inputFile := filepath.Join(*inputCrdsDir, crdFile.Name())
		crdBytes, err := ioutil.ReadFile(inputFile)
		if err != nil {
			panic(err)
		}

		fmt.Printf("reading CRD file %s\n", inputFile)

		entries := strings.Split(string(crdBytes), "---")
		for _, entry := range entries {
			crd := extv1beta1.CustomResourceDefinition{}
			err = yaml.Unmarshal([]byte(entry), &crd)
			if err != nil {
				panic(err)
			}
			if crd.Spec.Names.Singular == "" {
				// filters out empty entries caused by starting file with '---' separator
				continue
			}

			outputFile := filepath.Join(*outputDir, fmt.Sprintf("%s.crd.yaml", crd.Spec.Names.Singular))
			writer := strings.Builder{}
			marshallObject(crd, &writer)
			err := ioutil.WriteFile(outputFile, []byte(writer.String()), 0644)
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
	os.MkdirAll(*outputDir, os.FileMode(0755))
	os.MkdirAll(filepath.Join(*outputDir, "crds"), os.FileMode(0755))

	generateUnifiedCSV()
	copyCrds()
	copyManifests()
}
