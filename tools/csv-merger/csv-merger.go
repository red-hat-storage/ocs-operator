package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/api/pkg/lib/version"
	csvv1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
)

var (
	csvVersion         = flag.String("csv-version", "", "the unified CSV version")
	replacesCsvVersion = flag.String("replaces-csv-version", "", "the unified CSV version this new CSV will replace")
	skipRange          = flag.String("skip-range", "", "the CSV version skip range")
	ocsCSVStr          = flag.String("ocs-csv-filepath", "", "path to ocs csv yaml file")
	timestamp          = flag.String("timestamp", "false", "bool value to enable/disable timestamp changes in CSV")

	rookContainerImage       = flag.String("rook-image", "", "rook operator container image")
	cephContainerImage       = flag.String("ceph-image", "", "ceph daemon container image")
	noobaaCoreContainerImage = flag.String("noobaa-core-image", "", "noobaa core container image")
	noobaaDBContainerImage   = flag.String("noobaa-db-image", "", "db container image for noobaa")
	ocsContainerImage        = flag.String("ocs-image", "", "ocs operator container image")
	ocsMetricsExporterImage  = flag.String("ocs-metrics-exporter-image", "", "ocs metrics exporter container image")
	uxBackendOauthImage      = flag.String("ux-backend-oauth-image", "", "ux backend oauth container image")
	ocsMustGatherImage       = flag.String("ocs-must-gather-image", "", "ocs-must-gather image")
	kubeRbacProxyImage       = flag.String("kube-rbac-proxy-image", "", "kube-rbac-proxy container image")

	desiredCephxKeyGen = flag.Int("desired-cephx-key-gen", 2, "desired cephx keys generation")

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
	OcsOperatorCsvVersion string
	OcsOperatorImage      string
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
		OcsOperatorCsvVersion: *csvVersion,
		OcsOperatorImage:      *ocsContainerImage,
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
				Name:  "KUBE_RBAC_PROXY_IMAGE",
				Value: *kubeRbacProxyImage,
			},
			{
				Name:  "OCS_METRICS_EXPORTER_IMAGE",
				Value: *ocsMetricsExporterImage,
			},
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
				Name:  "ONBOARDING_VALIDATION_KEYS_GENERATOR_IMAGE",
				Value: *ocsContainerImage,
			},
			{
				Name:  "DESIRED_CEPHX_KEY_GEN",
				Value: strconv.Itoa(*desiredCephxKeyGen),
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

	s := string(yamlBytes)

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

	ocsCSV.Spec.CustomResourceDefinitions.Required = nil

	ocsCSV.Annotations["operators.operatorframework.io/internal-objects"] = ""

	templateStrategySpec := &ocsCSV.Spec.InstallStrategy.StrategySpec

	// set default mem/cpu requests/limits value in csv
	templateStrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("250m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
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
	if *timestamp == "true" {
		loc, err := time.LoadLocation("UTC")
		if err != nil {
			panic(err)
		}
		ocsCSV.Annotations["createdAt"] = time.Now().In(loc).Format("2006-01-02 15:04:05")
	}
	ocsCSV.Annotations["containerImage"] = *ocsContainerImage
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
	if *cephContainerImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "ceph-container",
			"image": *cephContainerImage,
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
	if *kubeRbacProxyImage != "" {
		relatedImages = append(relatedImages, map[string]interface{}{
			"name":  "kube-rbac-proxy",
			"image": *kubeRbacProxyImage,
		})
	}
	return unstructured.SetNestedSlice(r.Object, relatedImages, "spec", "relatedImages")
}

func copyCrds(ocsCSV *csvv1.ClusterServiceVersion) {
	var crdFiles []string
	crdDirs := []string{"ocs"}

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

		toJSON, err := yaml.YAMLToJSON(crdBytes)
		if err != nil {
			panic(err)
		}
		decoder := json.NewDecoder(bytes.NewReader(toJSON))
		for {
			crd := extv1.CustomResourceDefinition{}
			err = decoder.Decode(&crd)
			if err == io.EOF {
				break
			} else if err != nil {
				panic(err)
			}

			// add labels on crds which act as a storagesystem
			if crd.Name == "storageclusters.ocs.openshift.io" {
				if crd.Labels == nil {
					crd.Labels = map[string]string{}
				}
				crd.Labels["odf.openshift.io/is-storage-system"] = "true"
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

func getUXBackendServerDeployment() appsv1.DeploymentSpec {
	deployment := appsv1.DeploymentSpec{
		Replicas: ptr.To(int32(1)),
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
							{
								Name: util.PodNamespaceEnvVar,
								ValueFrom: &corev1.EnvVarSource{
									FieldRef: &corev1.ObjectFieldSelector{
										FieldPath: "metadata.namespace",
									},
								},
							},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("250m"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
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
								Optional:   ptr.To(true),
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
				PriorityClassName:  "system-cluster-critical",
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
	} else if *kubeRbacProxyImage == "" {
		log.Fatal("--kube-rbac-proxy-image is required")
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
