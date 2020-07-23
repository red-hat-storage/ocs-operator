package functests_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	scController "github.com/openshift/ocs-operator/pkg/controller/storagecluster"

	deploymanager "github.com/openshift/ocs-operator/pkg/deploy-manager"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/types"
	//"k8s.io/apimachinery/pkg/api/errors"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	resourceName = "storageclusterinitializations"
)

// constant names of each resource
const (
	CephObjectStoreUserType = "CephObjectStoreUser"
	CephObjectStoreType     = "CephObjectStore"
	StorageClassType        = "StorageClass"
	CephFilesystemType      = "CephFilesystem"
	CephBlockPoolType       = "CephBlockPool"
)

type SCInit struct {
	ocsClient            *rest.RESTClient
	parameterCodec       runtime.ParameterCodec
	name                 string
	namespace            string
	client               crclient.Client
	currentCloudPlatform scController.CloudPlatformType

	// This map is used to cross examine that objects maintain
	// the same UID after re-reconciling. This is important to
	// ensure objects are reconciled in place, and not deleted/recreated
	uidMap map[types.UID]string
}

func newSCInit() (*SCInit, error) {
	scInitObj := &SCInit{}

	// initialize 'uidMap'
	scInitObj.uidMap = make(map[types.UID]string)

	// initialize controller runtime client, 'client'
	clientScheme := scheme.Scheme
	cephv1.AddToScheme(clientScheme)
	conf, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	scInitObj.client, err = crclient.New(conf, crclient.Options{Scheme: clientScheme})
	if err != nil {
		return nil, err
	}

	// initialize the 'name' and 'namespace' with the default storagecluster
	defaultStorageCluster, err := deploymanager.DefaultStorageCluster()
	scInitObj.name = defaultStorageCluster.Name
	scInitObj.namespace = defaultStorageCluster.Namespace

	// initialize 'ocsClient', the rest-client
	deployManager, err := deploymanager.NewDeployManager()
	if err != nil {
		return nil, err
	}
	scInitObj.ocsClient = deployManager.GetOcsClient()

	// initialize 'parameterCodec', with the current runtime parameterCodec,
	// which is used for serializing and deserializing API objects to url values
	scInitObj.parameterCodec = deployManager.GetParameterCodec()

	// initialize 'currentCloudPlatform', if not cloud it will contain 'PlatformUnknown
	platform := &scController.CloudPlatform{}
	scInitObj.currentCloudPlatform, err = platform.GetPlatform(scInitObj.client)
	if err != nil {
		return nil, err
	}

	return scInitObj, nil
}

func (scInitObj *SCInit) getSCI() (*ocsv1.StorageClusterInitialization, error) {
	sci := &ocsv1.StorageClusterInitialization{}
	err := scInitObj.ocsClient.Get().
		Resource(resourceName).
		Namespace(scInitObj.namespace).
		Name(scInitObj.name).
		VersionedParams(&metav1.GetOptions{}, scInitObj.parameterCodec).
		Do().
		Into(sci)
	if err != nil {
		return nil, err
	}
	return sci, nil
}

func (scInitObj *SCInit) deleteResource() error {
	return scInitObj.ocsClient.Delete().
		Resource(resourceName).
		Namespace(scInitObj.namespace).
		Name(scInitObj.name).
		VersionedParams(&metav1.GetOptions{}, scInitObj.parameterCodec).
		Do().
		Error()
}

func (scInitObj *SCInit) createCephFilesystemExpect(expectDelete bool) *cephFilesystemExpect {
	cfsExpectObj := newCephFilesystemExpect(scInitObj)
	cfsExpectObj.ExpectDelete(expectDelete)
	return cfsExpectObj
}

func (scInitObj *SCInit) createCephObjectStoreUserExpect(expectDelete bool) *cephObjectStoreUserExpect {
	cosuExpectObj := newCephObjectStoreUserExpect(scInitObj)
	cosuExpectObj.ExpectDelete(expectDelete)
	return cosuExpectObj
}

func (scInitObj *SCInit) createCephBlockPoolExpect(expectDelete bool) *cephBlockPoolExpect {
	cbpExpect := newCephBlockPoolExpect(scInitObj)
	cbpExpect.ExpectDelete(expectDelete)
	return cbpExpect
}

func (scInitObj *SCInit) createCephObjectStoreExpect(expectDelete bool) *cephObjectStoreExpect {
	cbpExpect := newCephObjectStoreExpect(scInitObj)
	cbpExpect.ExpectDelete(expectDelete)
	return cbpExpect
}

func (scInitObj *SCInit) createStorageClassExpect(expectDelete bool) *storageClassExpect {
	scExpectObj := newStorageClassExpect(scInitObj)
	scExpectObj.ExpectDelete(expectDelete)
	return scExpectObj
}

/************************************************************************************/
///////////////////////////////cephFileSystemExpect///////////////////////////////////
/************************************************************************************/

type cephFilesystemExpect struct {
	scInitObj    *SCInit
	filesystem   *cephv1.CephFilesystem
	key          crclient.ObjectKey
	expectDelete bool
}

func newCephFilesystemExpect(scInitObj *SCInit) *cephFilesystemExpect {
	cfsExpectObj := &cephFilesystemExpect{expectDelete: false, scInitObj: scInitObj}
	cfsExpectObj.filesystem = &cephv1.CephFilesystem{}
	cfsExpectObj.key = crclient.ObjectKey{
		Namespace: scInitObj.namespace,
		Name:      fmt.Sprintf("%s-cephfilesystem", scInitObj.name),
	}
	return cfsExpectObj
}

func (cfsExpectObj *cephFilesystemExpect) ExpectResourceTypeName() string {
	return CephFilesystemType
}

func (cfsExpectObj *cephFilesystemExpect) ExpectDelete(expectDelete bool) {
	cfsExpectObj.expectDelete = expectDelete
}

func (cfsExpectObj *cephFilesystemExpect) ExpectReconcile() error {
	err := cfsExpectObj.scInitObj.client.Get(context.TODO(),
		cfsExpectObj.key,
		cfsExpectObj.filesystem)
	if err != nil {
		return err
	}
	if len(cfsExpectObj.filesystem.Spec.DataPools) > 0 &&
		cfsExpectObj.filesystem.Spec.DataPools[0].FailureDomain == "fake" {
		return fmt.Errorf("Waiting on ceph filesystem to reconcile changes")
	}
	if !cfsExpectObj.expectDelete {
		if _, ok := cfsExpectObj.scInitObj.uidMap[cfsExpectObj.filesystem.UID]; !ok {
			return fmt.Errorf("Could not find a CephFS mapped to UID: %s",
				cfsExpectObj.filesystem.UID)
		}
	}
	return nil
}

func (cfsExpectObj *cephFilesystemExpect) ExpectModify() error {
	if err := cfsExpectObj.scInitObj.client.Get(
		context.TODO(), cfsExpectObj.key, cfsExpectObj.filesystem); err != nil {
		return err
	}
	// modifying the failure domain
	if cfsExpectObj.expectDelete {
		if err := cfsExpectObj.scInitObj.client.Delete(
			context.TODO(), cfsExpectObj.filesystem); err != nil {
			return err
		}
	} else {
		cfsExpectObj.filesystem.Spec.DataPools[0].FailureDomain = "fake"
		if err := cfsExpectObj.scInitObj.client.Update(
			context.TODO(), cfsExpectObj.filesystem); err != nil {
			return err
		}
	}
	cfsExpectObj.scInitObj.uidMap[cfsExpectObj.filesystem.UID] = ""
	return nil
}

/************************************************************************************/
/////////////////////////////cephObjectStoreUserExpect////////////////////////////////
/************************************************************************************/

type cephObjectStoreUserExpect struct {
	scInitObj    *SCInit
	objStoreUser *cephv1.CephObjectStoreUser
	key          crclient.ObjectKey
	expectDelete bool
}

func newCephObjectStoreUserExpect(scInitObj *SCInit) *cephObjectStoreUserExpect {
	cosuExpectObj := &cephObjectStoreUserExpect{
		scInitObj:    scInitObj,
		expectDelete: false,
		objStoreUser: &cephv1.CephObjectStoreUser{},
	}
	cosuExpectObj.key = crclient.ObjectKey{
		Namespace: scInitObj.namespace,
		Name:      fmt.Sprintf("%s-cephobjectstoreuser", scInitObj.name),
	}
	return cosuExpectObj
}

func (cosuExpectObj *cephObjectStoreUserExpect) ExpectResourceTypeName() string {
	return CephObjectStoreUserType
}

func (cosuExpectObj *cephObjectStoreUserExpect) ExpectModify() error {
	err := cosuExpectObj.scInitObj.client.Get(context.TODO(),
		cosuExpectObj.key,
		cosuExpectObj.objStoreUser,
	)
	if err != nil {
		return err
	}
	// modifying the user store name
	if cosuExpectObj.expectDelete {
		if err := cosuExpectObj.scInitObj.client.Delete(
			context.TODO(), cosuExpectObj.objStoreUser); err != nil {
			return err
		}
	} else {
		cosuExpectObj.objStoreUser.Spec.Store = "fake"
		if err := cosuExpectObj.scInitObj.client.Update(
			context.TODO(), cosuExpectObj.objStoreUser); err != nil {
			return err
		}
	}
	cosuExpectObj.scInitObj.uidMap[cosuExpectObj.objStoreUser.UID] = ""
	return nil
}

func (cosuExpectObj *cephObjectStoreUserExpect) ExpectDelete(expectDelete bool) {
	cosuExpectObj.expectDelete = expectDelete
}

func (cosuExpectObj *cephObjectStoreUserExpect) ExpectReconcile() error {
	err := cosuExpectObj.scInitObj.client.Get(context.TODO(),
		cosuExpectObj.key,
		cosuExpectObj.objStoreUser)
	if err != nil {
		return err
	}
	if cosuExpectObj.objStoreUser.Spec.Store == "fake" {
		return fmt.Errorf("Waiting on ceph object store user to reconcile changes")
	}
	if !cosuExpectObj.expectDelete {
		_, ok := cosuExpectObj.scInitObj.uidMap[cosuExpectObj.objStoreUser.UID]
		if !ok {
			return fmt.Errorf("Could not find a CephObjectStore user mapping to UID: %s",
				cosuExpectObj.objStoreUser.UID)
		}
	}
	return nil
}

/************************************************************************************/
////////////////////////////////cephBlockPoolExpect///////////////////////////////////
/************************************************************************************/

type cephBlockPoolExpect struct {
	scInitObj    *SCInit
	blockPool    *cephv1.CephBlockPool
	key          crclient.ObjectKey
	expectDelete bool
}

func newCephBlockPoolExpect(scInitObj *SCInit) *cephBlockPoolExpect {
	cbpExpect := &cephBlockPoolExpect{expectDelete: false, scInitObj: scInitObj}
	cbpExpect.blockPool = &cephv1.CephBlockPool{}
	cbpExpect.key = crclient.ObjectKey{
		Namespace: scInitObj.namespace,
		Name:      fmt.Sprintf("%s-cephblockpool", scInitObj.name)}
	return cbpExpect
}

func (cbpExpect *cephBlockPoolExpect) ExpectResourceTypeName() string {
	return CephBlockPoolType
}

func (cbpExpect *cephBlockPoolExpect) ExpectDelete(expectDelete bool) {
	cbpExpect.expectDelete = expectDelete
}

func (cbpExpect *cephBlockPoolExpect) ExpectModify() error {
	err := cbpExpect.scInitObj.client.Get(context.TODO(),
		cbpExpect.key,
		cbpExpect.blockPool,
	)
	if err != nil {
		return err
	}
	// modifying the failure domain
	if cbpExpect.expectDelete {
		if err := cbpExpect.scInitObj.client.Delete(
			context.TODO(), cbpExpect.blockPool); err != nil {
			return err
		}
	} else {
		cbpExpect.blockPool.Spec.FailureDomain = "fake"
		if err := cbpExpect.scInitObj.client.Update(
			context.TODO(), cbpExpect.blockPool); err != nil {
			return err
		}
	}
	cbpExpect.scInitObj.uidMap[cbpExpect.blockPool.UID] = ""
	return nil
}

func (cbpExpect *cephBlockPoolExpect) ExpectReconcile() error {
	err := cbpExpect.scInitObj.client.Get(context.TODO(),
		cbpExpect.key,
		cbpExpect.blockPool,
	)
	if err != nil {
		return err
	}
	if cbpExpect.blockPool.Spec.FailureDomain == "fake" {
		return fmt.Errorf("Waiting on ceph block pool to reconcile changes")
	}
	if !cbpExpect.expectDelete {
		if _, ok := cbpExpect.scInitObj.uidMap[cbpExpect.blockPool.UID]; !ok {
			return fmt.Errorf("Could not find CephBlockPool mapped to UID: %s",
				cbpExpect.blockPool.UID)
		}
	}
	return nil
}

/************************************************************************************/
///////////////////////////////cephObjectStoreExpect//////////////////////////////////
/************************************************************************************/

type cephObjectStoreExpect struct {
	scInitObj    *SCInit
	objStore     *cephv1.CephObjectStore
	key          crclient.ObjectKey
	expectDelete bool
}

func newCephObjectStoreExpect(scInitObj *SCInit) *cephObjectStoreExpect {
	cosExpectObj := &cephObjectStoreExpect{
		scInitObj:    scInitObj,
		expectDelete: false,
		objStore:     &cephv1.CephObjectStore{},
	}
	cosExpectObj.key = crclient.ObjectKey{
		Namespace: scInitObj.namespace,
		Name:      fmt.Sprintf("%s-cephobjectstore", scInitObj.name)}
	return cosExpectObj
}

func (cosExpectObj *cephObjectStoreExpect) ExpectResourceTypeName() string {
	return CephObjectStoreType
}

func (cosExpectObj *cephObjectStoreExpect) ExpectDelete(expectDelete bool) {
	cosExpectObj.expectDelete = expectDelete
}

func (cosExpectObj *cephObjectStoreExpect) ExpectModify() error {
	if err := cosExpectObj.scInitObj.client.Get(context.TODO(),
		cosExpectObj.key, cosExpectObj.objStore); err != nil {
		return err
	}
	// modifying the gateway instance count
	if cosExpectObj.expectDelete {
		if err := cosExpectObj.scInitObj.client.Delete(context.TODO(),
			cosExpectObj.objStore); err != nil {
			return err
		}
	} else {
		cosExpectObj.objStore.Spec.Gateway.Instances = 5
		if err := cosExpectObj.scInitObj.client.Update(context.TODO(),
			cosExpectObj.objStore); err != nil {
			return err
		}
	}
	cosExpectObj.scInitObj.uidMap[cosExpectObj.objStore.UID] = ""
	return nil
}

func (cosExpectObj *cephObjectStoreExpect) ExpectReconcile() error {
	err := cosExpectObj.scInitObj.client.Get(context.TODO(),
		cosExpectObj.key,
		cosExpectObj.objStore,
	)
	if err != nil {
		return err
	}
	if cosExpectObj.objStore.Spec.Gateway.Instances == 5 {
		return fmt.Errorf("Waiting on ceph object store to reconcile changes")
	}
	if !cosExpectObj.expectDelete {
		if _, ok := cosExpectObj.scInitObj.uidMap[cosExpectObj.objStore.UID]; !ok {
			return fmt.Errorf("Could not find CephObjectPool object mapped to UID: %s",
				cosExpectObj.objStore.UID)
		}
	}
	return nil
}

/************************************************************************************/
/////////////////////////////////storageClassExpect///////////////////////////////////
/************************************************************************************/

type storageClassExpect struct {
	scInitObj    *SCInit
	storageClass *storagev1.StorageClass
	key          crclient.ObjectKey
	expectDelete bool
}

func newStorageClassExpect(scInitObj *SCInit) *storageClassExpect {
	scExpectObj := &storageClassExpect{
		scInitObj:    scInitObj,
		expectDelete: false,
		storageClass: &storagev1.StorageClass{}}
	scExpectObj.key = crclient.ObjectKey{
		Namespace: scInitObj.namespace,
		Name:      fmt.Sprintf("%s-ceph-rbd", scInitObj.name)}
	return scExpectObj
}

func (scExpectObj *storageClassExpect) ExpectResourceTypeName() string {
	return StorageClassType
}

func (scExpectObj *storageClassExpect) ExpectModify() error {
	err := scExpectObj.scInitObj.client.Get(context.TODO(),
		scExpectObj.key,
		scExpectObj.storageClass,
	)
	if err != nil {
		return err
	}
	if scExpectObj.expectDelete {
		if err := scExpectObj.scInitObj.client.Delete(
			context.TODO(), scExpectObj.storageClass); err != nil {
			return err
		}
	} else {
		// I couldn't find a storageClass field in the Spec that
		// the apiserver allows mutations on.
		// We'll still verify the UID doesn't change in during
		// the reconcile loop though.
	}
	scExpectObj.scInitObj.uidMap[scExpectObj.storageClass.UID] = ""
	return nil
}

func (scExpectObj *storageClassExpect) ExpectDelete(expectDelete bool) {
	scExpectObj.expectDelete = expectDelete
}

func (scExpectObj *storageClassExpect) ExpectReconcile() error {
	err := scExpectObj.scInitObj.client.Get(context.TODO(),
		scExpectObj.key,
		scExpectObj.storageClass,
	)
	if err != nil {
		return err
	}
	if !scExpectObj.expectDelete {
		if _, ok := scExpectObj.scInitObj.uidMap[scExpectObj.storageClass.UID]; !ok {
			return fmt.Errorf("StorageClass object was expected to be found")
		}
	}
	return nil
}

// ExpectTypeI an interface that
// brings a common test behavioural pattern
type ExpectTypeI interface {
	ExpectResourceTypeName() string
	ExpectModify() error
	ExpectDelete(bool)
	ExpectReconcile() error
}

func getAllValidExpectResources(scInitObj *SCInit, tobeDeleted bool) []ExpectTypeI {
	var expctRs = []ExpectTypeI{
		scInitObj.createCephFilesystemExpect(tobeDeleted),
		scInitObj.createStorageClassExpect(tobeDeleted),
	}
	// CephBlockPool can only be added if it is not to be deleted (ie; 'tobeDeleted' flag is false)
	// deletion of CephBlockPool disrupts 'noobaa'
	if !tobeDeleted {
		expctRs = append(expctRs, scInitObj.createCephBlockPoolExpect(tobeDeleted))
	}
	// add CephObjectStore and CephObjectStoreUser
	// only if the current platform is not a cloud one (not AWS or GCP or Azure),
	if scInitObj.currentCloudPlatform == scController.PlatformUnknown {
		expctRs = append(expctRs, scInitObj.createCephObjectStoreExpect(tobeDeleted))
		expctRs = append(expctRs, scInitObj.createCephObjectStoreUserExpect(tobeDeleted))
	}
	return expctRs
}

// at compile time itself, assert that all the test resources
// follow the 'ExpectTypeI' interface pattern
var _ = []ExpectTypeI{
	&cephFilesystemExpect{},
	&cephObjectStoreUserExpect{},
	&cephBlockPoolExpect{},
	&cephObjectStoreExpect{},
	&storageClassExpect{},
}

// main 'Describe' function
var _ = Describe("StorageClusterInitialization", StorageClusterInitializationTest)

func StorageClusterInitializationTest() {
	var scInitObj *SCInit

	BeforeEach(func() {
		RegisterFailHandler(Fail)

		var err error

		scInitObj, err = newSCInit()
		Expect(err).To(BeNil())

		Expect(scInitObj.currentCloudPlatform).To(BeElementOf(append(scController.ValidCloudPlatforms,
			scController.PlatformUnknown)))
	})

	deleteSCIAndWaitForCreate := func() {
		err := scInitObj.deleteResource()
		Expect(err).To(BeNil())
		Eventually(func() error {
			_, err := scInitObj.getSCI()
			if err != nil {
				return fmt.Errorf("Waiting on StorageClusterInitialization to be re-created: %v", err)
			}
			return nil
		}, 10*time.Second, 1*time.Second).ShouldNot(HaveOccurred())
	}

	Describe("verify re-initialization", func() {
		AfterEach(func() {

			// This helps ensure we attempt to restore the init resources
			// in the event of a test failure.
			// Otherwise it's possible a mutated or deleted init resource
			// could impact other tests
			deleteSCIAndWaitForCreate()
		})
		Context("after", func() {
			It("resources have been modified", func() {
				var err error
				var expectResources = getAllValidExpectResources(scInitObj, false)
				for _, eachResource := range expectResources {
					By(fmt.Sprintf("Modifying %s", eachResource.ExpectResourceTypeName()))
					err = eachResource.ExpectModify()
					Expect(err).To(BeNil())
				}

				By("Deleting StorageClusterInitialization and waiting for it to recreate")
				deleteSCIAndWaitForCreate()

				for _, eachResource := range expectResources {
					By(fmt.Sprintf("Verifying %s is reconciled",
						eachResource.ExpectResourceTypeName()))
					Eventually(eachResource.ExpectReconcile, 15*time.Second,
						1*time.Second).ShouldNot(HaveOccurred())
				}
			})

			It("resources have been deleted", func() {
				var err error
				var expectResources = getAllValidExpectResources(scInitObj, true)
				for _, eachResource := range expectResources {
					By(fmt.Sprintf("Modifying %s (expected to be deleted)",
						eachResource.ExpectResourceTypeName()))
					err = eachResource.ExpectModify()
					Expect(err).To(BeNil())
				}

				By("Deleting StorageClusterInitialization and waiting for it to recreate")
				deleteSCIAndWaitForCreate()

				for _, eachResource := range expectResources {
					By(fmt.Sprintf("Verifying %s is reconciled (after deletion)",
						eachResource.ExpectResourceTypeName()))
					Eventually(eachResource.ExpectReconcile, 15*time.Second,
						1*time.Second).ShouldNot(HaveOccurred())
				}
			})
		})
	})
}
