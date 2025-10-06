package storagecluster

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	nadclientset "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
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

	tolerations := getPlacement(sc, "toolbox").Tolerations
	nodeAffinity := getPlacement(sc, "toolbox").NodeAffinity

	toolsDeployment := newToolsDeployment(namespace, tolerations, nodeAffinity)
	foundToolsDeployment := &appsv1.Deployment{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: rookCephToolDeploymentName, Namespace: namespace}, foundToolsDeployment)

	if err == nil {
		isFound = true
	} else if errors.IsNotFound(err) {
		isFound = false
	} else {
		return err
	}

	// Add default resource requirements, or respect custom resource definitions under storagecluster spec.
	toolsDeployment.Spec.Template.Spec.Containers[0].Resources = getDaemonResources("rook-ceph-tools", sc)

	if sc.Spec.EnableCephTools {
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
		if shouldUseHostNetworking(sc) {
			toolsDeployment.Spec.Template.Spec.HostNetwork = true
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

func newToolsDeployment(namespace string, tolerations []corev1.Toleration, nodeAffinity *corev1.NodeAffinity) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rookCephToolDeploymentName,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "rook-ceph-tools",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "rook-ceph-tools",
					},
				},
				Spec: corev1.PodSpec{
					DNSPolicy:          corev1.DNSClusterFirstWithHostNet,
					ServiceAccountName: "rook-ceph-default",
					Containers: []corev1.Container{
						{
							Name:    rookCephToolDeploymentName,
							Image:   os.Getenv("ROOK_CEPH_IMAGE"),
							Command: []string{"/bin/bash"},
							Args: []string{
								"-m",
								"-c",
								"/usr/local/bin/toolbox.sh",
							},
							TTY: true,
							Env: []corev1.EnvVar{
								{
									Name: "ROOK_CEPH_USERNAME",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
											Key:                  "ceph-username",
										},
									},
								},
								{
									Name: "ROOK_CEPH_SECRET",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
											Key:                  "ceph-secret",
										},
									},
								},
							},
							SecurityContext: &corev1.SecurityContext{
								RunAsNonRoot: ptr.To(true),
								RunAsUser:    ptr.To(int64(2016)),
								RunAsGroup:   ptr.To(int64(2016)),
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "ceph-config", MountPath: "/etc/ceph"},
								{Name: "mon-endpoint-volume", MountPath: "/etc/rook"},
							},
						},
					},
					Tolerations: tolerations,
					Affinity: &corev1.Affinity{
						NodeAffinity: nodeAffinity,
					},
					Volumes: []corev1.Volume{
						{Name: "ceph-config", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: "mon-endpoint-volume", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon-endpoints"},
								Items: []corev1.KeyToPath{
									{Key: "data", Path: "mon-endpoints"},
								},
							},
						},
						},
					},
				},
			},
		},
	}
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
