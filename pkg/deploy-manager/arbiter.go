package deploymanager

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetArbiterZone returns the elected arbiter zone.
func (t *DeployManager) GetArbiterZone() string {
	return t.storageClusterConf.arbiterConf.Zone
}

// electArbiterZone is a helper function just for the tests. In real deployments, the arbiter zone will be picked by the user.
// In our tests, we will just pick the zone with the least number of nodes as the arbiter zone.
func (t *DeployManager) electArbiterZone() error {
	var arbiterZoneElect string

	nodes, err := t.k8sClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: "node-role.kubernetes.io/worker"})
	if err != nil {
		return err
	}
	zoneNodesCount := make(map[string]int)
	for _, node := range nodes.Items {
		fmt.Printf("node zone is %v\n", node.GetLabels()[corev1.LabelZoneFailureDomainStable])
		zoneNodesCount[node.GetLabels()[corev1.LabelZoneFailureDomainStable]]++
	}

	// Let a random zone be the arbiter zone
	for zone := range zoneNodesCount {
		arbiterZoneElect = zone
	}

	// In this second pass over the map, we will find the zone with the least number of nodes
	for zone := range zoneNodesCount {
		if zoneNodesCount[zone] < zoneNodesCount[arbiterZoneElect] {
			arbiterZoneElect = zone
		}
	}
	t.storageClusterConf.arbiterConf.Zone = arbiterZoneElect
	return nil
}

// ArbiterEnabled returns true if the arbiter configuration is enabled in the deploy manager
func (t *DeployManager) ArbiterEnabled() bool {
	return t.storageClusterConf.arbiterConf.Enabled
}

// EnableArbiter sets the arbiter configuration to true in the deploy manager.
// The arbiter zone is automatically determied at the time of cluster deploy.
func (t *DeployManager) EnableArbiter() {
	t.storageClusterConf.arbiterConf.Enabled = true
}
