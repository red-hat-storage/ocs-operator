package storagecluster

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	nadclientset "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (

	// RookCephToolDeploymentName is the name of the rook-ceph-tools deployment
	rookCephToolDeploymentName = "rook-ceph-tools"
)

func (r *StorageClusterReconciler) ensureToolsDeployment(sc *ocsv1.StorageCluster) error {

	reconcileStrategy := ReconcileStrategy(sc.Spec.ManagedResources.CephToolbox.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return nil
	}
	var isFound bool
	namespace := sc.Namespace

	tolerations := []corev1.Toleration{{
		Key:      defaults.NodeTolerationKey,
		Operator: corev1.TolerationOpEqual,
		Value:    "true",
		Effect:   corev1.TaintEffectNoSchedule,
	}}

	// Get the ocsinitialization CR
	ocsinit := &ocsv1.OCSInitialization{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: "ocsinit", Namespace: namespace}, ocsinit)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	// Get tolerations from both the storagecluster CR & ocsinitialization CR
	// Keeping this for ability to add tolerations from ocsinitialization CR
	tolerations = append(tolerations, getPlacement(sc, "toolbox").Tolerations...)
	tolerations = append(tolerations, ocsinit.Spec.Tolerations...)

	//Remove duplicate tolerations
	tolerations = removeDuplicateTolerations(tolerations)

	toolsDeployment := sc.NewToolsDeployment(tolerations)
	foundToolsDeployment := &appsv1.Deployment{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: rookCephToolDeploymentName, Namespace: namespace}, foundToolsDeployment)

	if err == nil {
		isFound = true
	} else if errors.IsNotFound(err) {
		isFound = false
	} else {
		return err
	}

	// Checking both ocsinitialization & storagecluster CR for their Enablecephtools field
	if sc.Spec.EnableCephTools || ocsinit.Spec.EnableCephTools {
		// Create or Update if ceph tools is enabled.

		//Adding Ownerreference to the ceph tools
		err = controllerutil.SetOwnerReference(sc, toolsDeployment, r.Client.Scheme())
		if err != nil {
			return err
		}

		// Add multus related annotations, if multus provider is set
		if isMultus(sc.Spec.Network) {
			net, err := getMultusPublicNetwork(sc)
			if err != nil {
				return err
			}
			if net != "" {
				if toolsDeployment.Spec.Template.ObjectMeta.Annotations == nil {
					toolsDeployment.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
				}
				toolsDeployment.Spec.Template.ObjectMeta.Annotations["k8s.v1.cni.cncf.io/networks"] = net
				toolsDeployment.Spec.Template.Spec.HostNetwork = false
			}
		}

		if !isFound {
			return r.Client.Create(context.TODO(), toolsDeployment)
		} else if !reflect.DeepEqual(foundToolsDeployment.Spec, toolsDeployment.Spec) {

			updateDeployment := foundToolsDeployment.DeepCopy()
			updateDeployment.Spec = *toolsDeployment.Spec.DeepCopy()

			return r.Client.Update(context.TODO(), updateDeployment)
		}
	} else if isFound {
		// delete if ceph tools exists and is disabled
		return r.Client.Delete(context.TODO(), foundToolsDeployment)
	}
	return nil
}

func removeDuplicateTolerations(tolerations []corev1.Toleration) []corev1.Toleration {
	keys := make(map[corev1.Toleration]bool)
	list := []corev1.Toleration{}
	for _, entry := range tolerations {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

func getMultusPublicNetwork(sc *ocsv1.StorageCluster) (string, error) {
	err := validateMultusSelectors(sc.Spec.Network.Selectors)
	if err != nil {
		return "", err
	}

	if sc.Spec.Network.Selectors["public"] == "" {
		return "", nil
	}

	multusNetName := sc.Spec.Network.Selectors["public"]
	multusNetNamespacedName := strings.Split(multusNetName, "/")
	nadNS, nadName := "", ""
	if len(multusNetNamespacedName) == 1 {
		nadNS, nadName = os.Getenv(util.OperatorNamespaceEnvVar), multusNetName
	} else if len(multusNetNamespacedName) == 2 {
		nadNS, nadName = multusNetNamespacedName[0], multusNetNamespacedName[1]
	} else {
		return "", fmt.Errorf("Spec.Network.Selectors[\"public\"] value: %s in storagecluster CR is invalid", multusNetName)
	}

	nadClient, err := getNADClient()
	if err != nil {
		return "", fmt.Errorf("failed to get NAD client. %v", err)
	}
	_, err = nadClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(nadNS).Get(context.TODO(), nadName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("Either create the NetworkAttachmentDefinition or alter the storagecluster Spec.Network.Selectors[\"public\"] with correct value. %v", err)
	}

	return fmt.Sprintf("%s/%s", nadNS, nadName), nil
}

func getNADClient() (*nadclientset.Clientset, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		return nil, fmt.Errorf("failed to build config for NAD. %v", err)
	}
	client, err := nadclientset.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create a new Clientset for NAD. %v", err)
	}
	return client, nil
}
