package types_test

import (
	"comparealerts/types"
	"comparealerts/utils"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	yamlContent1_sha256 = [sha256.Size]byte{
		236, 128, 229, 111, 16, 116, 128, 254,
		13, 61, 36, 133, 51, 174, 128, 193,
		37, 52, 243, 255, 199, 64, 199, 27,
		166, 40, 241, 136, 131, 214, 142, 69}
	yamlContent2_sha256 = [sha256.Size]byte{
		50, 218, 70, 55, 125, 182, 63, 40,
		71, 211, 93, 63, 82, 57, 95, 181,
		159, 235, 85, 156, 118, 229, 146, 221,
		254, 25, 155, 137, 186, 141, 227, 44}
)

var (
	expectedDiffs1 = []types.PrometheusRuleDiffUnit{
		{Alert: "OdfMirrorDaemonStatus2", DiffReasons: []types.DiffReasonSubUnit{
			{DiffReason: types.DifferentExpr, DiffMessage: types.DifferentExpr.String(),
				Rule1: []types.Rule{{Alert: "OdfMirrorDaemonStatus2",
					Expr: types.IntOrString{IntOrString: intstr.IntOrString{
						Type:   intstr.String,
						StrVal: `((count by(namespace) (ocs_mirror_daemon_count{job="ocs-metrics-exporter"} == 0)) * on(namespace) group_left() (count by(namespace) (ocs_pool_mirroring_status{job="ocs-metrics-exporter"} == 1))) > 0`}, LineNo: 19}, For: "1m", Labels: map[string]string{"severity": "critical"},
					Annotations: map[string]string{
						"description":    "Mirror daemon is in unhealthy status for more than 1m. Mirroring on this cluster is not working as expected.",
						"message":        "Mirror daemon is unhealthy.",
						"severity_level": "error", "storage_type": "ceph"}}},
				Rule2: []types.Rule{{Alert: "OdfMirrorDaemonStatus2",
					Expr: types.IntOrString{IntOrString: intstr.IntOrString{Type: intstr.String, StrVal: `((count by(namespace) (ocs_mirror_daemon_count{job="ocs-metrics-exporter"} == 0)) * on(namespace) group_left() (count by(namespace) (ocs_pool_mirroring_status{job="ocs-metrics-exporter"} == 1))) > 3`}, LineNo: 19}, For: "1m", Labels: map[string]string{"severity": "critical"}, Annotations: map[string]string{"description": "Mirror daemon is in unhealthy status for more than 1m. Mirroring on this cluster is not working as expected.", "message": "Mirror daemon is unhealthy.", "severity_level": "error", "storage_type": "ceph"}}}},
		}},
		{Alert: "OdfPoolMirroringImageHealth", DiffReasons: []types.DiffReasonSubUnit{{
			DiffReason:  types.AlertOnlyWithMe,
			DiffMessage: types.AlertOnlyWithMe.String(),
			Rule1: []types.Rule{{
				Alert: "OdfPoolMirroringImageHealth",
				Expr:  types.IntOrString{IntOrString: intstr.IntOrString{Type: intstr.String, StrVal: `(ocs_pool_mirroring_image_health{job="ocs-metrics-exporter"}  * on (namespace) group_left() (max by(namespace) (ocs_pool_mirroring_status{job="ocs-metrics-exporter"}))) == 1`}, LineNo: 41},
				For:   "1m", Labels: map[string]string{"severity": "warning"},
				Annotations: map[string]string{"description": "Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Unknown state for more than 1m. Mirroring might not work as expected.", "message": "Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Unknown state.", "severity_level": "warning", "storage_type": "ceph"}}},
			Rule2: []types.Rule{}}}},
	}
	expectedDiffs2 = []types.PrometheusRuleDiffUnit{
		{
			Alert: "CephMgrIsMissingReplicas",
			DiffReasons: []types.DiffReasonSubUnit{
				{
					DiffReason:  types.DifferentExpr,
					DiffMessage: "difference in expression",
					Rule1: []types.Rule{
						{
							Alert: "CephMgrIsMissingReplicas",
							Expr: types.IntOrString{
								IntOrString: intstr.IntOrString{Type: intstr.String, StrVal: `sum(kube_deployment_spec_replicas{deployment=~"rook-ceph-mgr-.*"}) by (namespace) < 1`},
								LineNo:      1,
							},
							For:         "5m",
							Labels:      map[string]string{"severity": "warning"},
							Annotations: map[string]string{"description": "Ceph Manager is missing replicas.", "message": "Storage metrics collector service doesn't have required no of replicas.", "severity_level": "warning", "storage_type": "ceph"},
						},
					},
					Rule2: []types.Rule{
						{
							Alert: "CephMgrIsMissingReplicas",
							Expr: types.IntOrString{
								IntOrString: intstr.IntOrString{Type: intstr.String, StrVal: `ABC > 10`},
								LineNo:      1,
							},
							For:         "5m",
							Labels:      map[string]string{"severity": "warning"},
							Annotations: map[string]string{"description": "Ceph Manager is missing replicas.", "message": "Storage metrics collector service doesn't have required no of replicas.", "severity_level": "warning", "storage_type": "ceph"},
						},
					},
				},
			},
		},
	}
)

func testDataIntegrity(t *testing.T) {
	var testData = []string{yamlContent1, yamlContent2}
	var expectedSha256 = [][sha256.Size]byte{
		yamlContent1_sha256, yamlContent2_sha256}
	for idx, tData := range testData {
		if actualSha256 := sha256.Sum256([]byte(tData)); expectedSha256[idx] != actualSha256 {
			t.Errorf("Expected: %+v Actual: %+v", expectedSha256[idx], actualSha256)
			t.Errorf("test content%d changed", idx+1)
			t.FailNow()
		}
	}
}

func TestNewPrometheusRuleFromFile(t *testing.T) {
	_, err := types.NewPrometheusRuleFromFile("non-existing-file.txt")
	if err == nil {
		t.Errorf("Function 'NewPrometheusRuleFromFile()' is supposed to throw an error")
		t.FailNow()
	}
	testDataIntegrity(t)

	prObj1 := promRuleObj.DeepCopy()
	jsonStr1 := jsonDataFromMonPrometheusRule(t, prObj1)
	prObj2 := promRuleObj.DeepCopy()
	changeAnAlertExpr("CephMgrIsMissingReplicas", "ABC > 10", prObj2)
	jsonStr2 := jsonDataFromMonPrometheusRule(t, prObj2)

	testDataArr := []struct {
		fileContent1, fileContent2 *string
		fileType                   string
		expecedDiff                []types.PrometheusRuleDiffUnit
	}{
		{fileContent1: &yamlContent1, fileContent2: &yamlContent2,
			fileType: "yaml", expecedDiff: expectedDiffs1},
		{fileContent1: &jsonStr1, fileContent2: &jsonStr2, fileType: "json", expecedDiff: expectedDiffs2},
	}
	for _, td := range testDataArr {
		tmpFile1 := createATempFile(t, "tmpPromRule", td.fileType, td.fileContent1)
		tmpFile2 := createATempFile(t, "tmpPromRule", td.fileType, td.fileContent2)
		defer func() { os.Remove(tmpFile1); os.Remove(tmpFile2) }()
		promRule1, err := types.NewPrometheusRuleFromFile(tmpFile1)
		if err != nil {
			t.Errorf("Failed to create a prometheus rule object from file: %s", tmpFile1)
			t.Errorf("Error: %v", err)
			t.FailNow()
		}
		promRule2, err := types.NewPrometheusRuleFromFile(tmpFile2)
		if err != nil {
			t.Errorf("Failed to create a prometheus rule object from file: %s", tmpFile2)
			t.Errorf("Error: %v", err)
			t.FailNow()
		}
		diffs := promRule1.Diff(promRule2)
		compareDiffs(t, diffs, td.expecedDiff)
	}
}

func createATempFile(t *testing.T, prefix, ext string, content *string) string {
	if content == nil {
		t.Errorf("We don't have any contents")
		t.FailNow()
	}
	f1, err := os.CreateTemp("", fmt.Sprintf("%s*.%s", prefix, ext))
	if err != nil {
		t.Error("Creating a temporary file failed")
		t.FailNow()
	}
	defer f1.Close()
	if _, err := f1.WriteString(*content); err != nil {
		t.Errorf("Writing to the tmp file failed: %s", f1.Name())
		os.Remove(f1.Name())
		t.FailNow()
	}
	return f1.Name()
}

/*
func printDiffDetails(t *testing.T, prDiffUnit *types.PrometheusRuleDiffUnit) {
	t.Logf("Alert: %s", prDiffUnit.Alert)
	for _, diffReason := range prDiffUnit.DiffReasons {
		t.Logf("Diff Reason: %v", diffReason.DiffReason)
		t.Logf("Diff Message: %v", diffReason.DiffMessage)
		t.Logf("RuleSet Upstream: %+v", diffReason.Rule1)
		t.Logf("RuleSet Downstream: %+v", diffReason.Rule2)
	}
}
*/

func compareDiffs(t *testing.T, diffs1, diffs2 []types.PrometheusRuleDiffUnit) {
	if diff1Len, diff2Len := len(diffs1), len(diffs2); diff1Len != diff2Len {
		t.Errorf("Length of the diffs don't match. Diff1Len: %d Diff2Len: %d",
			diff1Len, diff2Len)
		t.FailNow()
	}
	for _, diff1 := range diffs1 {
		alertNameNotFound := true
		for _, diff2 := range diffs2 {
			if diff1.Alert != diff2.Alert {
				continue
			}
			alertNameNotFound = false
			for rIndx, diff1Reason := range diff1.DiffReasons {
				diff2Reason := diff2.DiffReasons[rIndx]
				if diff1Reason.DiffReason != diff2Reason.DiffReason {
					t.Errorf("Diff reasons don't match: R1: %v R2: %v",
						diff1Reason.DiffReason, diff2Reason.DiffReason)
					t.FailNow()
				}
				if len(diff1Reason.Rule1) != len(diff2Reason.Rule1) {
					t.Errorf("No: of rules in RuleSet-1 don't match")
					t.FailNow()
				}
				if len(diff1Reason.Rule2) != len(diff2Reason.Rule2) {
					t.Errorf("No: of rules in RuleSet-2 don't match")
					t.FailNow()
				}
				for ruleIndx, diff1Rule1 := range diff1Reason.Rule1 {
					diff2Rule1 := diff2Reason.Rule1[ruleIndx]
					expr1 := utils.TrimWithOnlySpaces(diff1Rule1.Expr.String())
					expr2 := utils.TrimWithOnlySpaces(diff2Rule1.Expr.String())
					if expr1 != expr2 {
						t.Errorf("RuleSet-1 expressions don't match:\nExpr1: %s\nExpr2: %s", expr1, expr2)
						t.FailNow()
					}
				}
				for ruleIndx, diff1Rule2 := range diff1Reason.Rule2 {
					diff2Rule2 := diff2Reason.Rule2[ruleIndx]
					expr1 := utils.TrimWithOnlySpaces(diff1Rule2.Expr.String())
					expr2 := utils.TrimWithOnlySpaces(diff2Rule2.Expr.String())
					if expr1 != expr2 {
						t.Errorf("RuleSet-2 expressions don't match:\nExpr1: %s\nExpr2: %s", expr1, expr2)
						t.FailNow()
					}
				}
			}
		}
		if alertNameNotFound {
			t.Errorf("Alert name: %s not found", diff1.Alert)
			t.FailNow()
		}
	}
}
