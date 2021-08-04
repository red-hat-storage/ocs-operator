package storagecluster

import (
	apiv2 "github.com/operator-framework/api/pkg/operators/v2"
	"github.com/operator-framework/operator-lib/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// returns a Conditions interface to Get or Set OperatorConditions
func NewUpgradeable(cl client.Client) (conditions.Condition, error) {
	return conditions.NewCondition(cl, apiv2.ConditionType(apiv2.Upgradeable))
}
