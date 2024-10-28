package deploymanager

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	operatorv1 "github.com/operator-framework/api/pkg/operators/v1"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/controller/install"
	appsv1 "k8s.io/api/apps/v1"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	yaml "sigs.k8s.io/yaml"
)

const marketplaceNamespace = "openshift-marketplace"

type clusterObjects struct {
	namespaces     []k8sv1.Namespace
	configmaps     []k8sv1.ConfigMap
	operatorGroups []operatorv1.OperatorGroup
	catalogSources []operatorv1alpha1.CatalogSource
	subscriptions  []operatorv1alpha1.Subscription
}

func (t *DeployManager) deployClusterObjects(co *clusterObjects) error {

	for _, namespace := range co.namespaces {
		err := t.CreateNamespace(namespace.Name)
		if err != nil {
			return err
		}
	}

	for _, cm := range co.configmaps {
		if err := t.Client.Create(context.TODO(), &cm); err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
	}

	for _, operatorGroup := range co.operatorGroups {
		operatorGroups := &operatorv1.OperatorGroupList{}
		err := t.Client.List(context.TODO(), operatorGroups, &client.ListOptions{Namespace: operatorGroup.Namespace})
		if err != nil {
			return err
		}
		if len(operatorGroups.Items) > 1 {
			// There should be only one operatorgroup in a namespace.
			// The system is already misconfigured - error out.
			return fmt.Errorf("More than one operatorgroup detected in namespace %v - aborting", operatorGroup.Namespace)
		}
		if len(operatorGroups.Items) > 0 {
			// There should be only one operatorgroup in a namespace.
			// Skip this one, so we don't make the system bad.
			continue
		}
		err = t.Client.Create(context.TODO(), &operatorGroup)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
	}

	for _, catalogSource := range co.catalogSources {
		err := t.Client.Create(context.TODO(), &catalogSource)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
	}

	// Wait for catalog source before posting subscription
	err := t.waitForOCSCatalogSource()
	if err != nil {
		return err
	}

	for _, subscription := range co.subscriptions {
		err = t.Client.Create(context.TODO(), &subscription)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
	}

	// Wait on ocs-operator, rook-ceph-operator and noobaa-operator to come online.
	err = t.WaitForOCSOperator()
	if err != nil {
		return err
	}

	return nil
}

func (t *DeployManager) generateClusterObjects(ocsCatalogImage string, subscriptionChannel string) *clusterObjects {

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

	// Configmaps
	co.configmaps = append(co.configmaps, k8sv1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-client-operator-config",
			Namespace: InstallNamespace,
		},
		Data: map[string]string{
			// lifted from hack/install-ocs-client.sh
			"DEPLOY_CSI": "false",
		},
	})

	// Operator Groups
	ocsOG := operatorv1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-operatorgroup",
			Namespace: InstallNamespace,
		},
		Spec: operatorv1.OperatorGroupSpec{
			TargetNamespaces: []string{InstallNamespace},
		},
	}
	ocsOG.SetGroupVersionKind(schema.GroupVersionKind{Group: operatorv1.SchemeGroupVersion.Group, Kind: "OperatorGroup", Version: operatorv1.SchemeGroupVersion.Version})

	co.operatorGroups = append(co.operatorGroups, ocsOG)

	// Catalog Sources
	ocsCatalog := operatorv1alpha1.CatalogSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-catalogsource",
			Namespace: marketplaceNamespace,
		},
		Spec: operatorv1alpha1.CatalogSourceSpec{
			SourceType:  operatorv1alpha1.SourceTypeGrpc,
			Image:       ocsCatalogImage,
			DisplayName: "OpenShift Container Storage",
			Publisher:   "Red Hat",
			Icon: operatorv1alpha1.Icon{
				Data:      "PHN2ZyBpZD0iTGF5ZXJfMSIgZGF0YS1uYW1lPSJMYXllciAxIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAxOTIgMTQ1Ij48ZGVmcz48c3R5bGU+LmNscy0xe2ZpbGw6I2UwMDt9PC9zdHlsZT48L2RlZnM+PHRpdGxlPlJlZEhhdC1Mb2dvLUhhdC1Db2xvcjwvdGl0bGU+PHBhdGggZD0iTTE1Ny43Nyw2Mi42MWExNCwxNCwwLDAsMSwuMzEsMy40MmMwLDE0Ljg4LTE4LjEsMTcuNDYtMzAuNjEsMTcuNDZDNzguODMsODMuNDksNDIuNTMsNTMuMjYsNDIuNTMsNDRhNi40Myw2LjQzLDAsMCwxLC4yMi0xLjk0bC0zLjY2LDkuMDZhMTguNDUsMTguNDUsMCwwLDAtMS41MSw3LjMzYzAsMTguMTEsNDEsNDUuNDgsODcuNzQsNDUuNDgsMjAuNjksMCwzNi40My03Ljc2LDM2LjQzLTIxLjc3LDAtMS4wOCwwLTEuOTQtMS43My0xMC4xM1oiLz48cGF0aCBjbGFzcz0iY2xzLTEiIGQ9Ik0xMjcuNDcsODMuNDljMTIuNTEsMCwzMC42MS0yLjU4LDMwLjYxLTE3LjQ2YTE0LDE0LDAsMCwwLS4zMS0zLjQybC03LjQ1LTMyLjM2Yy0xLjcyLTcuMTItMy4yMy0xMC4zNS0xNS43My0xNi42QzEyNC44OSw4LjY5LDEwMy43Ni41LDk3LjUxLjUsOTEuNjkuNSw5MCw4LDgzLjA2LDhjLTYuNjgsMC0xMS42NC01LjYtMTcuODktNS42LTYsMC05LjkxLDQuMDktMTIuOTMsMTIuNSwwLDAtOC40MSwyMy43Mi05LjQ5LDI3LjE2QTYuNDMsNi40MywwLDAsMCw0Mi41Myw0NGMwLDkuMjIsMzYuMywzOS40NSw4NC45NCwzOS40NU0xNjAsNzIuMDdjMS43Myw4LjE5LDEuNzMsOS4wNSwxLjczLDEwLjEzLDAsMTQtMTUuNzQsMjEuNzctMzYuNDMsMjEuNzdDNzguNTQsMTA0LDM3LjU4LDc2LjYsMzcuNTgsNTguNDlhMTguNDUsMTguNDUsMCwwLDEsMS41MS03LjMzQzIyLjI3LDUyLC41LDU1LC41LDc0LjIyYzAsMzEuNDgsNzQuNTksNzAuMjgsMTMzLjY1LDcwLjI4LDQ1LjI4LDAsNTYuNy0yMC40OCw1Ni43LTM2LjY1LDAtMTIuNzItMTEtMjcuMTYtMzAuODMtMzUuNzgiLz48L3N2Zz4=",
				MediaType: "image/svg+xml",
			},
		},
	}
	ocsCatalog.SetGroupVersionKind(schema.GroupVersionKind{Group: v1alpha1.SchemeGroupVersion.Group, Kind: "CatalogSource", Version: v1alpha1.SchemeGroupVersion.Version})

	co.catalogSources = append(co.catalogSources, ocsCatalog)

	// Subscriptions
	ocsSubscription := v1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-subscription",
			Namespace: InstallNamespace,
		},
		Spec: &v1alpha1.SubscriptionSpec{
			Channel:                subscriptionChannel,
			Package:                "ocs-operator",
			CatalogSource:          "ocs-catalogsource",
			CatalogSourceNamespace: marketplaceNamespace,
		},
	}
	ocsSubscription.SetGroupVersionKind(schema.GroupVersionKind{Group: v1alpha1.SchemeGroupVersion.Group, Kind: "Subscription", Version: v1alpha1.SchemeGroupVersion.Version})

	rookSubscription := v1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rook-subscription",
			Namespace: InstallNamespace,
		},
		Spec: &v1alpha1.SubscriptionSpec{
			Channel:                subscriptionChannel,
			Package:                "rook-ceph-operator",
			CatalogSource:          "ocs-catalogsource",
			CatalogSourceNamespace: marketplaceNamespace,
		},
	}
	rookSubscription.SetGroupVersionKind(schema.GroupVersionKind{Group: v1alpha1.SchemeGroupVersion.Group, Kind: "Subscription", Version: v1alpha1.SchemeGroupVersion.Version})

	noobaSubscription := v1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nooba-subscription",
			Namespace: InstallNamespace,
		},
		Spec: &v1alpha1.SubscriptionSpec{
			Channel:                subscriptionChannel,
			Package:                "noobaa-operator",
			CatalogSource:          "ocs-catalogsource",
			CatalogSourceNamespace: marketplaceNamespace,
		},
	}
	noobaSubscription.SetGroupVersionKind(schema.GroupVersionKind{Group: v1alpha1.SchemeGroupVersion.Group, Kind: "Subscription", Version: v1alpha1.SchemeGroupVersion.Version})

	co.subscriptions = append(co.subscriptions, ocsSubscription, rookSubscription, noobaSubscription)

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
func (t *DeployManager) DumpYAML(ocsCatalogImage string, subscriptionChannel string) string {
	co := t.generateClusterObjects(ocsCatalogImage, subscriptionChannel)

	writer := strings.Builder{}

	for _, namespace := range co.namespaces {
		err := marshallObject(namespace, &writer)
		if err != nil {
			panic(err)
		}
	}

	for _, cm := range co.configmaps {
		err := marshallObject(cm, &writer)
		if err != nil {
			panic(err)
		}
	}

	for _, operatorGroup := range co.operatorGroups {
		err := marshallObject(operatorGroup, &writer)
		if err != nil {
			panic(err)
		}
	}

	for _, catalogSource := range co.catalogSources {
		err := marshallObject(catalogSource, &writer)
		if err != nil {
			panic(err)
		}
	}

	for _, subscription := range co.subscriptions {
		err := marshallObject(subscription, &writer)
		if err != nil {
			panic(err)
		}
	}

	return writer.String()
}

func (t *DeployManager) waitForOCSCatalogSource() error {
	timeout := 300 * time.Second
	interval := 10 * time.Second
	ctx := context.TODO()

	lastReason := ""

	labelSelector, err := labels.Parse("olm.catalogSource in (ocs-catalogsource)")
	if err != nil {
		return err
	}

	err = utilwait.PollUntilContextTimeout(ctx, interval, timeout, true, func(context.Context) (done bool, err error) {
		pods := &k8sv1.PodList{}
		err = t.Client.List(context.TODO(), pods, &client.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			lastReason = fmt.Sprintf("error talking to k8s apiserver: %v", err)
			return false, nil
		}

		if len(pods.Items) == 0 {
			lastReason = "waiting on ocs catalog source pod to be created"
			return false, nil
		}
		isReady := false
		for _, pod := range pods.Items {
			for _, condition := range pod.Status.Conditions {
				if condition.Type == k8sv1.PodReady && condition.Status == k8sv1.ConditionTrue {
					isReady = true
					break
				}
			}
		}

		if !isReady {
			lastReason = "waiting on ocs catalog source pod to reach ready state"
			return false, nil
		}

		// if we get here, then all deployments are created and available
		return true, nil
	})

	if err != nil {
		return fmt.Errorf("%v: %s", err, lastReason)
	}

	return nil
}

// DeployOCSWithOLM deploys ocs operator via an olm subscription
func (t *DeployManager) DeployOCSWithOLM(ocsCatalogImage string, subscriptionChannel string) error {

	if ocsCatalogImage == "" {
		return fmt.Errorf("catalog registry images not supplied")
	}

	co := t.generateClusterObjects(ocsCatalogImage, subscriptionChannel)
	err := t.deployClusterObjects(co)
	if err != nil {
		return err
	}

	return nil
}

// UpgradeOCSWithOLM upgrades ocs operator via an olm subscription
func (t *DeployManager) UpgradeOCSWithOLM(ocsCatalogImage string, subscriptionChannel string) error {

	if ocsCatalogImage == "" {
		return fmt.Errorf("catalog registry images not supplied")
	}

	co := t.generateClusterObjects(ocsCatalogImage, subscriptionChannel)
	err := t.updateClusterObjects(co)
	if err != nil {
		return err
	}

	return nil
}

// WaitForOCSOperator waits for the ocs-operator to come online
func (t *DeployManager) WaitForOCSOperator() error {
	deployments := []string{"ocs-operator", "rook-ceph-operator", "noobaa-operator"}

	timeout := 1000 * time.Second
	interval := 10 * time.Second
	ctx := context.TODO()

	lastReason := ""

	err := utilwait.PollUntilContextTimeout(ctx, interval, timeout, true, func(context.Context) (done bool, err error) {
		for _, name := range deployments {
			deployment := &appsv1.Deployment{}
			err = t.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: InstallNamespace}, deployment)
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

// UninstallOCS uninstalls ocs operator and storage clusters
func (t *DeployManager) UninstallOCS(ocsCatalogImage string, subscriptionChannel string) error {
	// Delete remaining operator manifests
	co := t.generateClusterObjects(ocsCatalogImage, subscriptionChannel)
	err := t.deleteClusterObjects(co)
	if err != nil {
		return err
	}
	for _, namespace := range co.namespaces {
		err = t.DeleteNamespaceAndWait(namespace.Name)
		if err != nil {
			return err
		}

	}

	return nil
}

func (t *DeployManager) deleteClusterObjects(co *clusterObjects) error {

	for _, operatorGroup := range co.operatorGroups {
		err := t.Client.Delete(context.TODO(), &operatorGroup)
		if err != nil && !errors.IsNotFound(err) {
			return err
		}

	}

	for _, catalogSource := range co.catalogSources {
		err := t.Client.Delete(context.TODO(), &catalogSource)
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	for _, subscription := range co.subscriptions {
		err := t.Client.Delete(context.TODO(), &subscription)
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (t *DeployManager) updateClusterObjects(co *clusterObjects) error {
	for _, catalogSource := range co.catalogSources {
		cs := &operatorv1alpha1.CatalogSource{}
		err := t.Client.Get(context.TODO(), types.NamespacedName{Name: catalogSource.Name, Namespace: catalogSource.Namespace}, cs)
		if err != nil {
			return err
		}
		cs.Spec.Image = catalogSource.Spec.Image
		err = t.Client.Update(context.TODO(), cs)
		if err != nil {
			return err
		}

	}

	// TODO: Verify this is a new catalog source. But does it have to be a new catalogsource?
	// Can we upgrade to a new subscription channel?
	// Wait for catalog source before updating subscription
	err := t.waitForOCSCatalogSource()
	if err != nil {
		return err
	}

	for _, subscription := range co.subscriptions {
		sub := &operatorv1alpha1.Subscription{}
		err := t.Client.Get(context.TODO(), types.NamespacedName{Name: subscription.Name, Namespace: subscription.Namespace}, sub)
		if err != nil {
			return err
		}
		sub.Spec.Channel = subscription.Spec.Channel
		err = t.Client.Update(context.TODO(), sub)
		if err != nil {
			return err
		}

	}
	return nil
}

// WaitForCsvUpgrade waits for the catalogsource to come online after an upgrade
func (t *DeployManager) WaitForCsvUpgrade(csvName string, subscriptionChannel string) error {
	timeout := 1200 * time.Second
	// NOTE the long timeout above. It can take quite a bit of time for the
	// ocs operator deployments to roll out
	interval := 10 * time.Second
	ctx := context.TODO()

	subscription := "ocs-subscription"
	operatorName := "ocs-operator"

	lastReason := ""
	waitErr := utilwait.PollUntilContextTimeout(ctx, interval, timeout, true, func(context.Context) (done bool, err error) {
		sub := &operatorv1alpha1.Subscription{}
		err = t.Client.Get(context.TODO(), types.NamespacedName{Name: subscription, Namespace: InstallNamespace}, sub)
		if err != nil {
			return false, err
		}
		if sub.Spec.Channel != subscriptionChannel {
			lastReason = fmt.Sprintf("waiting on subscription channel to be updated to %s ", subscriptionChannel)
			return false, nil
		}
		csvs := &operatorv1alpha1.ClusterServiceVersionList{}
		err = t.Client.List(context.TODO(), csvs, &client.ListOptions{Namespace: InstallNamespace})
		if err != nil {
			return false, err
		}
		for _, csv := range csvs.Items {
			// If the csvName doesn't match, it means a new csv has appeared
			if csv.Name != csvName && strings.Contains(csv.Name, operatorName) {
				// New csv found and phase is succeeded
				if csv.Status.Phase == "Succeeded" {
					return true, nil
				}
			}
		}
		lastReason = fmt.Sprintf("waiting on csv to be created and installed") //nolint:gosimple
		return false, nil
	})

	if waitErr != nil {
		return fmt.Errorf("%v: %s", waitErr, lastReason)
	}

	return nil
}

// GetCsv retrieves the csv named ocs-operator
func (t *DeployManager) GetCsv() (v1alpha1.ClusterServiceVersion, error) {
	csvName := "ocs-operator"
	csv := operatorv1alpha1.ClusterServiceVersion{}
	csvs := &operatorv1alpha1.ClusterServiceVersionList{}
	err := t.Client.List(context.TODO(), csvs, &client.ListOptions{Namespace: InstallNamespace})
	for _, csv := range csvs.Items {
		if strings.Contains(csv.Name, csvName) {
			return csv, err
		}
	}
	return csv, err
}

// VerifyComponentOperators makes sure that deployment images matches the ones specified in the csv deployment specs
func (t *DeployManager) VerifyComponentOperators() error {
	csv, err := t.GetCsv()
	if err != nil {
		return err
	}

	var resolver *install.StrategyResolver
	strategy, err := resolver.UnmarshalStrategy(csv.Spec.InstallStrategy)
	if err != nil {
		return err
	}

	strategyDetailsDeployment, _ := strategy.(*v1alpha1.StrategyDetailsDeployment)
	for _, deployment := range strategyDetailsDeployment.DeploymentSpecs {
		image := deployment.Spec.Template.Spec.Containers[0].Image
		foundImage, err := t.GetDeploymentImage(deployment.Name)
		if err != nil {
			return err
		}
		if image != foundImage {
			return fmt.Errorf("Deployment: %s Expected image: %s Found image  %s", deployment.Name, image, foundImage)
		}
	}
	return nil
}
