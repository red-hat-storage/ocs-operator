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

	cephFilesystemModify := func(shouldDelete bool) {
		filesystem := &cephv1.CephFilesystem{}
		key := crclient.ObjectKey{Namespace: scInitObj.namespace,
			Name: fmt.Sprintf("%s-cephfilesystem", scInitObj.name)}
		err := scInitObj.client.Get(context.TODO(),
			key,
			filesystem,
		)
		Expect(err).To(BeNil())

		// modifying the failure domain
		if shouldDelete {
			err = scInitObj.client.Delete(context.TODO(), filesystem)
			Expect(err).To(BeNil())
		} else {
			filesystem.Spec.DataPools[0].FailureDomain = "fake"
			err = scInitObj.client.Update(context.TODO(), filesystem)
			Expect(err).To(BeNil())

		}
		scInitObj.uidMap[filesystem.UID] = ""
	}

	cephFilesystemExpectReconcile := func(expectDelete bool) {
		filesystem := &cephv1.CephFilesystem{}
		key := crclient.ObjectKey{Namespace: scInitObj.namespace,
			Name: fmt.Sprintf("%s-cephfilesystem", scInitObj.name)}
		Eventually(func() error {
			err := scInitObj.client.Get(context.TODO(),
				key,
				filesystem,
			)
			if err != nil {
				return err
			}

			if filesystem.Spec.DataPools[0].FailureDomain == "fake" {
				return fmt.Errorf("Waiting on ceph filesystem to reconcile changes")
			}

			if !expectDelete {
				_, ok := scInitObj.uidMap[filesystem.UID]
				Expect(ok).To(BeTrue())
			}

			return nil
		}, 15*time.Second, 1*time.Second).ShouldNot(HaveOccurred())
	}

	cephObjectStoreUserModify := func(shouldDelete bool) {
		objStoreUser := &cephv1.CephObjectStoreUser{}
		key := crclient.ObjectKey{Namespace: scInitObj.namespace, Name: fmt.Sprintf("%s-cephobjectstoreuser", scInitObj.name)}
		err := scInitObj.client.Get(context.TODO(),
			key,
			objStoreUser,
		)
		Expect(err).To(BeNil())

		// modifying the user store name
		if shouldDelete {

			err = scInitObj.client.Delete(context.TODO(), objStoreUser)
			Expect(err).To(BeNil())
		} else {
			objStoreUser.Spec.Store = "fake"
			err = scInitObj.client.Update(context.TODO(), objStoreUser)
			Expect(err).To(BeNil())
		}
		scInitObj.uidMap[objStoreUser.UID] = ""
	}

	cephObjectStoreUserExpectReconcile := func(expectDelete bool) {
		objStoreUser := &cephv1.CephObjectStoreUser{}
		key := crclient.ObjectKey{Namespace: scInitObj.namespace, Name: fmt.Sprintf("%s-cephobjectstoreuser", scInitObj.name)}

		Eventually(func() error {
			err := scInitObj.client.Get(context.TODO(),
				key,
				objStoreUser,
			)
			if err != nil {
				return err
			}

			if objStoreUser.Spec.Store == "fake" {
				return fmt.Errorf("Waiting on ceph object store user to reconcile changes")
			}

			if !expectDelete {
				_, ok := scInitObj.uidMap[objStoreUser.UID]
				Expect(ok).To(BeTrue())
			}

			return nil
		}, 15*time.Second, 1*time.Second).ShouldNot(HaveOccurred())
	}

	cephBlockPoolModify := func(shouldDelete bool) {
		blockPool := &cephv1.CephBlockPool{}
		key := crclient.ObjectKey{Namespace: scInitObj.namespace, Name: fmt.Sprintf("%s-cephblockpool", scInitObj.name)}
		err := scInitObj.client.Get(context.TODO(),
			key,
			blockPool,
		)
		Expect(err).To(BeNil())

		// modifying the failure domain
		if shouldDelete {
			err = scInitObj.client.Delete(context.TODO(), blockPool)
			Expect(err).To(BeNil())
		} else {
			blockPool.Spec.FailureDomain = "fake"
			err = scInitObj.client.Update(context.TODO(), blockPool)
			Expect(err).To(BeNil())
		}

		scInitObj.uidMap[blockPool.UID] = ""
	}

	cephBlockPoolExpectReconcile := func(expectDelete bool) {
		blockPool := &cephv1.CephBlockPool{}
		key := crclient.ObjectKey{Namespace: scInitObj.namespace, Name: fmt.Sprintf("%s-cephblockpool", scInitObj.name)}

		Eventually(func() error {
			err := scInitObj.client.Get(context.TODO(),
				key,
				blockPool,
			)
			if err != nil {
				return err
			}

			if blockPool.Spec.FailureDomain == "fake" {
				return fmt.Errorf("Waiting on ceph block pool to reconcile changes")
			}

			if !expectDelete {
				_, ok := scInitObj.uidMap[blockPool.UID]
				Expect(ok).To(BeTrue())
			}

			return nil
		}, 15*time.Second, 1*time.Second).ShouldNot(HaveOccurred())
	}

	cephObjectStoreModify := func(shouldDelete bool) {
		objStore := &cephv1.CephObjectStore{}
		key := crclient.ObjectKey{Namespace: scInitObj.namespace, Name: fmt.Sprintf("%s-cephobjectstore", scInitObj.name)}
		err := scInitObj.client.Get(context.TODO(),
			key,
			objStore,
		)
		Expect(err).To(BeNil())

		// modifying the gateway instance count
		if shouldDelete {
			err = scInitObj.client.Delete(context.TODO(), objStore)
			Expect(err).To(BeNil())
		} else {
			objStore.Spec.Gateway.Instances = 5
			err = scInitObj.client.Update(context.TODO(), objStore)
			Expect(err).To(BeNil())
		}

		scInitObj.uidMap[objStore.UID] = ""
	}

	cephObjectStoreExpectReconcile := func(expectDelete bool) {
		objStore := &cephv1.CephObjectStore{}
		key := crclient.ObjectKey{Namespace: scInitObj.namespace, Name: fmt.Sprintf("%s-cephobjectstore", scInitObj.name)}

		Eventually(func() error {
			err := scInitObj.client.Get(context.TODO(),
				key,
				objStore,
			)
			if err != nil {
				return err
			}

			if objStore.Spec.Gateway.Instances == 5 {
				return fmt.Errorf("Waiting on ceph object store to reconcile changes")
			}

			if !expectDelete {
				_, ok := scInitObj.uidMap[objStore.UID]
				Expect(ok).To(BeTrue())
			}

			return nil
		}, 15*time.Second, 1*time.Second).ShouldNot(HaveOccurred())
	}

	storageClassModify := func(shouldDelete bool) {
		storageClass := &storagev1.StorageClass{}
		key := crclient.ObjectKey{Namespace: scInitObj.namespace, Name: fmt.Sprintf("%s-ceph-rbd", scInitObj.name)}
		err := scInitObj.client.Get(context.TODO(),
			key,
			storageClass,
		)
		Expect(err).To(BeNil())

		if shouldDelete {
			err = scInitObj.client.Delete(context.TODO(), storageClass)
			Expect(err).To(BeNil())
		} else {
			// I couldn't find a storageClass field in the Spec that
			// the apiserver allows mutations on.
			// We'll still verify the UID doesn't change in during
			// the reconcile loop though.
		}

		scInitObj.uidMap[storageClass.UID] = ""
	}

	storageClassExpectReconcile := func(expectDelete bool) {
		storageClass := &storagev1.StorageClass{}
		key := crclient.ObjectKey{Namespace: scInitObj.namespace, Name: fmt.Sprintf("%s-ceph-rbd", scInitObj.name)}

		Eventually(func() error {
			err := scInitObj.client.Get(context.TODO(),
				key,
				storageClass,
			)
			if err != nil {
				return err
			}

			if !expectDelete {
				_, ok := scInitObj.uidMap[storageClass.UID]
				Expect(ok).To(BeTrue())
			}

			return nil
		}, 15*time.Second, 1*time.Second).ShouldNot(HaveOccurred())
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
				if scInitObj.currentCloudPlatform != scController.PlatformAWS {
					By("Modifying CephObjectStore")
					cephObjectStoreModify(false)
					By("Modifying CephObjectStoreUser")
					cephObjectStoreUserModify(false)
				}

				By("Modifying StorageClass")
				storageClassModify(false)
				By("Modifying CephBlockPool")
				cephBlockPoolModify(false)
				By("Modifying CephFilesystem")
				cephFilesystemModify(false)

				By("Deleting StorageClusterInitialization and waiting for it to recreate")
				deleteSCIAndWaitForCreate()

				By("Verifying StorageClass is reconciled")
				storageClassExpectReconcile(false)
				if scInitObj.currentCloudPlatform != scController.PlatformAWS {
					By("Verifying CephObjectStore is reconciled")
					cephObjectStoreExpectReconcile(false)
					By("Verifying CephObjectStoreUser is reconciled")
					cephObjectStoreUserExpectReconcile(false)
				}
				By("Verifying CephBlockPool is reconciled")
				cephBlockPoolExpectReconcile(false)
				By("Verifying CephFilesystem is reconciled")
				cephFilesystemExpectReconcile(false)
			})

			It("resources have been deleted", func() {
				if scInitObj.currentCloudPlatform != scController.PlatformAWS {
					By("Modifying CephObjectStore")
					cephObjectStoreModify(true)
					By("Modifying CephObjectStoreUser")
					cephObjectStoreUserModify(true)
				}
				By("Modifying StorageClass")
				storageClassModify(true)
				// We can't delete the block pool because it disrupts noobaa
				//By("Modifying CephBlockPool")
				//cephBlockPoolModify(true)
				By("Modifying CephFilesystem")
				cephFilesystemModify(true)

				By("Deleting StorageClusterInitialization and waiting for it to recreate")
				deleteSCIAndWaitForCreate()

				By("Verifying StorageClass is reconciled")
				storageClassExpectReconcile(true)
				if scInitObj.currentCloudPlatform != scController.PlatformAWS {
					By("Verifying CephObjectStore is reconciled")
					cephObjectStoreExpectReconcile(true)
					By("Verifying CephObjectStoreUser is reconciled")
					cephObjectStoreUserExpectReconcile(true)
				}
				// We can't delete the block pool because it disrupts noobaa
				//By("Verifying CephBlockPool is reconciled")
				//cephBlockPoolExpectReconcile(true)
				By("Verifying CephFilesystem is reconciled")
				cephFilesystemExpectReconcile(true)
			})
		})
	})
}
