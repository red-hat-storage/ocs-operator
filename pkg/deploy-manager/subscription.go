package deploymanager

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	yaml "github.com/ghodss/yaml"
	v1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1"
	v1alpha1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
)

const localStorageNamespace = "local-storage"
const marketplaceNamespace = "openshift-marketplace"
const defaultLocalStorageRegistryImage = "quay.io/gnufied/local-registry:v4.2.0"
const defaultOcsRegistryImage = "quay.io/ocs-dev/ocs-registry:latest"

type clusterObjects struct {
	namespaces     []k8sv1.Namespace
	operatorGroups []v1.OperatorGroup
	catalogSources []v1alpha1.CatalogSource
	subscriptions  []v1alpha1.Subscription
}

func (t *DeployManager) deployClusterObjects(co *clusterObjects) error {

	for _, namespace := range co.namespaces {
		err := t.CreateNamespace(namespace.Name)
		if err != nil {
			return err
		}
	}

	for _, operatorGroup := range co.operatorGroups {
		_, err := t.olmClient.OperatorsV1().OperatorGroups(operatorGroup.Namespace).Create(&operatorGroup)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}

	}

	for _, catalogSource := range co.catalogSources {
		_, err := t.olmClient.OperatorsV1alpha1().CatalogSources(catalogSource.Namespace).Create(&catalogSource)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}

	}
	for _, subscription := range co.subscriptions {
		_, err := t.olmClient.OperatorsV1alpha1().Subscriptions(subscription.Namespace).Create(&subscription)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}

	}
	return nil
}

func (t *DeployManager) generateClusterObjects(ocsRegistryImage string, localStorageRegistryImage string) *clusterObjects {

	co := &clusterObjects{}
	label := make(map[string]string)
	// Label required for monitoring this namespace
	label["openshift.io/cluster-monitoring"] = "true"

	// Namespaces
	co.namespaces = append(co.namespaces, k8sv1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   InstallNamespace,
			Labels: label,
		},
	})
	co.namespaces = append(co.namespaces, k8sv1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: localStorageNamespace,
		},
	})

	// Operator Groups
	ocsOG := v1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "openshift-storage-operatorgroup",
			Namespace: InstallNamespace,
		},
		Spec: v1.OperatorGroupSpec{
			TargetNamespaces: []string{InstallNamespace},
		},
	}
	ocsOG.SetGroupVersionKind(schema.GroupVersionKind{Group: v1.SchemeGroupVersion.Group, Kind: "OperatorGroup", Version: v1.SchemeGroupVersion.Version})

	localStorageOG := v1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "local-operator-group",
			Namespace: localStorageNamespace,
		},
		Spec: v1.OperatorGroupSpec{
			TargetNamespaces: []string{InstallNamespace},
		},
	}
	localStorageOG.SetGroupVersionKind(schema.GroupVersionKind{Group: v1.SchemeGroupVersion.Group, Kind: "OperatorGroup", Version: v1.SchemeGroupVersion.Version})

	co.operatorGroups = append(co.operatorGroups, ocsOG)
	co.operatorGroups = append(co.operatorGroups, localStorageOG)

	// CatalogSources
	localStorageCatalog := v1alpha1.CatalogSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "local-storage-manifests",
			Namespace: marketplaceNamespace,
		},
		Spec: v1alpha1.CatalogSourceSpec{
			SourceType:  v1alpha1.SourceTypeGrpc,
			Image:       localStorageRegistryImage,
			DisplayName: "Local Storage Operator",
			Publisher:   "Red Hat",
			Description: "An operator to manage local volumes",
		},
	}
	localStorageCatalog.SetGroupVersionKind(schema.GroupVersionKind{Group: v1alpha1.SchemeGroupVersion.Group, Kind: "CatalogSource", Version: v1alpha1.SchemeGroupVersion.Version})

	ocsCatalog := v1alpha1.CatalogSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-catalogsource",
			Namespace: marketplaceNamespace,
		},
		Spec: v1alpha1.CatalogSourceSpec{
			SourceType:  v1alpha1.SourceTypeGrpc,
			Image:       ocsRegistryImage,
			DisplayName: "Openshift Container Storage",
			Publisher:   "Red Hat",
		},
	}
	ocsCatalog.SetGroupVersionKind(schema.GroupVersionKind{Group: v1alpha1.SchemeGroupVersion.Group, Kind: "CatalogSource", Version: v1alpha1.SchemeGroupVersion.Version})

	co.catalogSources = append(co.catalogSources, localStorageCatalog)
	co.catalogSources = append(co.catalogSources, ocsCatalog)

	// Subscriptions
	ocsSubscription := v1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-subscription",
			Namespace: InstallNamespace,
		},
		Spec: &v1alpha1.SubscriptionSpec{
			Channel:                "alpha",
			Package:                "ocs-operator",
			CatalogSource:          "ocs-catalogsource",
			CatalogSourceNamespace: marketplaceNamespace,
		},
	}
	ocsSubscription.SetGroupVersionKind(schema.GroupVersionKind{Group: v1alpha1.SchemeGroupVersion.Group, Kind: "Subscription", Version: v1alpha1.SchemeGroupVersion.Version})

	co.subscriptions = append(co.subscriptions, ocsSubscription)

	return co
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

	unstructured.RemoveNestedField(r.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(r.Object, "status")

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

// DumpYAML dumps ocs deployment yaml
func (t *DeployManager) DumpYAML(ocsRegistryImage string, localStorageRegistryImage string) string {
	co := t.generateClusterObjects(ocsRegistryImage, localStorageRegistryImage)

	writer := strings.Builder{}

	for _, namespace := range co.namespaces {
		marshallObject(namespace, &writer)
	}

	for _, operatorGroup := range co.operatorGroups {
		marshallObject(operatorGroup, &writer)
	}

	for _, catalogSource := range co.catalogSources {
		marshallObject(catalogSource, &writer)
	}

	for _, subscription := range co.subscriptions {
		marshallObject(subscription, &writer)
	}

	return writer.String()
}

// DeployOCSWithOLM deploys ocs operator via an olm subscription
func (t *DeployManager) DeployOCSWithOLM(ocsRegistryImage string, localStorageRegistryImage string) error {

	if ocsRegistryImage == "" || localStorageRegistryImage == "" {
		return fmt.Errorf("catalog registry images not supplied")
	}

	co := t.generateClusterObjects(ocsRegistryImage, localStorageRegistryImage)
	err := t.deployClusterObjects(co)
	if err != nil {
		return err
	}

	return nil
}

// WaitForOCSOperator waits for the ocs-operator to come online
func (t *DeployManager) WaitForOCSOperator() error {
	deployments := []string{"ocs-operator", "rook-ceph-operator", "noobaa-operator"}

	timeout := 1200 * time.Second
	// NOTE the long timeout above. It can take quite a bit of time for the
	// ocs operator deployments to roll out
	interval := 10 * time.Second

	lastReason := ""

	err := utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {
		for _, name := range deployments {
			deployment, err := t.k8sClient.AppsV1().Deployments(InstallNamespace).Get(name, metav1.GetOptions{})
			if err != nil {
				lastReason = fmt.Sprintf("waiting on deployment %s to be created", name)
				return false, nil
			}

			isAvailable := false
			for _, condition := range deployment.Status.Conditions {
				if condition.Type == appsv1.DeploymentAvailable && condition.Status == k8sv1.ConditionTrue {
					isAvailable = true
					break
				}
			}

			if !isAvailable {
				lastReason = fmt.Sprintf("waiting on deployment %s to become available", name)
				return false, nil
			}
		}

		// if we get here, then all deployments are created and available
		return true, nil
	})

	if err != nil {
		return fmt.Errorf("%v: %s", err, lastReason)
	}

	return nil
}
