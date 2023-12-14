package test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RHsyseng/operator-utils/pkg/validation"
	"github.com/ghodss/yaml"
	v1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"

	"github.com/stretchr/testify/assert"
)

const (
	pathResources            = "/spec/storageDeviceSets/resources/"
	pathNodeAffinity         = "/spec/storageDeviceSets/placement/nodeAffinity/"
	pathPodAffinity          = "/spec/storageDeviceSets/placement/podAffinity/"
	pathPodAntiAffinity      = "/spec/storageDeviceSets/placement/podAntiAffinity/"
	pathTolerations          = "/spec/storageDeviceSets/placement/tolerations/"
	pathDataPVCTemplate      = "/spec/storageDeviceSets/dataPVCTemplate/"
	pathStatusConditions     = "/status/conditions/"
	pathStatusNoobaaSystem   = "/status/noobaaSystemCreated"
	pathStatusRelatedObjs    = "/status/relatedObjects/"
	pathStatusNodeTopologies = "/status/nodeTopologies/"
	pathSpecMonPVCTemplate   = "/spec/monPVCTemplate/"
	pathLabelSelector        = "/spec/labelSelector"
	pathPlacement            = "/spec/placement"
	pathMetadataPVCTemplate  = "/spec/storageDeviceSets/metadataPVCTemplate/"
	pathWalPVCTemplate       = "/spec/storageDeviceSets/walPVCTemplate/"
)

func TestSampleCustomResources(t *testing.T) {
	t.Skip("Skipping CR Validation")
	root := "./../deploy/crds"
	crdCrMap := map[string]string{
		"ocs.openshift.io_ocsinitializations_crd.yaml": "ocs_v1_ocsinitialization_cr",
		"ocs.openshift.io_storageclusters_crd.yaml":    "ocs_v1_storagecluster_cr",
	}
	for crd, prefix := range crdCrMap {
		validateCustomResources(t, root, crd, prefix)
	}
}

// nolint
func validateCustomResources(t *testing.T, root string, crd string, prefix string) {
	schema := getSchema(t, fmt.Sprintf("%s/%s", root, crd))
	assert.NotNil(t, schema)
	walkFunc := func(path string, info os.FileInfo, err error) error {
		if strings.HasSuffix(info.Name(), "crd.yaml") {
			//Ignore CRD
			return nil
		}
		if strings.HasPrefix(info.Name(), prefix) {
			bytes, err := os.ReadFile(path)
			assert.NoError(t, err, "Error reading CR yaml from %v", path)
			var input map[string]interface{}
			assert.NoError(t, yaml.Unmarshal(bytes, &input))
			assert.NoError(t, schema.Validate(input), "File %v does not validate against the %s CRD schema", info.Name(), crd)
		}
		return nil
	}
	err := filepath.Walk(root, walkFunc)
	assert.NoError(t, err, "Error reading CR yaml files from ", root)
}

func TestCompleteCRD(t *testing.T) {
	t.Skip("Skipping CRD Validation")
	root := "./../deploy/crds"
	crdStructMap := map[string]interface{}{
		"ocs.openshift.io_ocsinitializations_crd.yaml": &v1.OCSInitialization{},
		"ocs.openshift.io_storageclusters_crd.yaml":    &v1.StorageCluster{},
	}
	for crd, obj := range crdStructMap {
		schema := getSchema(t, fmt.Sprintf("%s/%s", root, crd))
		missingEntries := schema.GetMissingEntries(obj)
		nestedOmissions := [...]string{
			// skip nested objects beneath each top-level object
			pathResources,
			pathNodeAffinity,
			pathPodAffinity,
			pathPodAntiAffinity,
			pathTolerations,
			pathDataPVCTemplate,
			pathStatusConditions,
			pathStatusNoobaaSystem,
			pathStatusRelatedObjs,
			pathSpecMonPVCTemplate,
			pathStatusNodeTopologies,
			pathLabelSelector,
			pathPlacement,
			pathMetadataPVCTemplate,
			pathWalPVCTemplate,
		}
		for _, missing := range missingEntries {
			skipAsOmission := false
			for _, omit := range nestedOmissions {
				if strings.HasPrefix(missing.Path, omit) {
					skipAsOmission = true
					break
				}
			}
			if !skipAsOmission {
				assert.Fail(t, "Discrepancy between CRD and Struct", "CRD: %s: Missing or incorrect schema validation at %s, expected type %s", crd, missing.Path, missing.Type)
			}
		}
	}
}

// nolint
func getSchema(t *testing.T, crd string) validation.Schema {
	bytes, err := os.ReadFile(crd)
	assert.NoError(t, err, "Error reading CRD yaml from %v", crd)
	schema, err := validation.New(bytes)
	assert.NoError(t, err)
	return schema
}
