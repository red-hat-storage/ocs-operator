package functests_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
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

var _ = Describe("StorageClusterInitialization", func() {
	var ocsClient *rest.RESTClient
	var parameterCodec runtime.ParameterCodec
	var name string
	var namespace string
	var client crclient.Client
	var currentCloudPlatform scController.CloudPlatformType
	platform := &scController.CloudPlatform{}

	// This map is used to cross examine that objects maintain
	// the same UID after re-reconciling. This is important to
	// ensure objects are reconciled in place, and not deleted/recreated
	var uidMap map[types.UID]string

	BeforeEach(func() {
		RegisterFailHandler(Fail)

		var err error

		clientScheme := scheme.Scheme
		cephv1.AddToScheme(clientScheme)

		uidMap = make(map[types.UID]string)

		defaultStorageCluster, err := deploymanager.DefaultStorageCluster()
		name = defaultStorageCluster.Name
		namespace = defaultStorageCluster.Namespace

		deployManager, err := deploymanager.NewDeployManager()
		Expect(err).To(BeNil())

		ocsClient = deployManager.GetOcsClient()
		parameterCodec = deployManager.GetParameterCodec()

		config, err := config.GetConfig()
		Expect(err).To(BeNil())

		client, err = crclient.New(config, crclient.Options{Scheme: clientScheme})
		Expect(err).To(BeNil())

		currentCloudPlatform, err = platform.GetPlatform(client)
		Expect(err).To(BeNil())
		Expect(currentCloudPlatform).To(BeElementOf(append(scController.ValidCloudPlatforms, scController.PlatformUnknown)))
	})

	getSCI := func() (*ocsv1.StorageClusterInitialization, error) {
		sci := &ocsv1.StorageClusterInitialization{}
		err := ocsClient.Get().
			Resource("storageclusterinitializations").
			Namespace(namespace).
			Name(name).
			VersionedParams(&metav1.GetOptions{}, parameterCodec).
			Do().
			Into(sci)
		return sci, err
	}

	deleteSCIAndWaitForCreate := func() {
		err := ocsClient.Delete().
			Resource("storageclusterinitializations").
			Namespace(namespace).
			Name(name).
			VersionedParams(&metav1.GetOptions{}, parameterCodec).
			Do().
			Error()
		if err != nil {
			Expect(err).To(BeNil())
		}

		gomega.Eventually(func() error {
			_, err := getSCI()
			if err != nil {
				return fmt.Errorf("Waiting on StorageClusterInitialization to be re-created: %v", err)
			}
			return nil
		}, 10*time.Second, 1*time.Second).ShouldNot(gomega.HaveOccurred())
	}

	cephFilesystemModify := func(shouldDelete bool) {
		filesystem := &cephv1.CephFilesystem{}
		key := crclient.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("%s-cephfilesystem", name)}
		err := client.Get(context.TODO(),
			key,
			filesystem,
		)
		Expect(err).To(BeNil())

		// modifying the failure domain
		if shouldDelete {
			err = client.Delete(context.TODO(), filesystem)
			Expect(err).To(BeNil())
		} else {
			filesystem.Spec.DataPools[0].FailureDomain = "fake"
			err = client.Update(context.TODO(), filesystem)
			Expect(err).To(BeNil())

		}
		uidMap[filesystem.UID] = ""
	}

	cephFilesystemExpectReconcile := func(expectDelete bool) {
		filesystem := &cephv1.CephFilesystem{}
		key := crclient.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("%s-cephfilesystem", name)}

		gomega.Eventually(func() error {
			err := client.Get(context.TODO(),
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
				_, ok := uidMap[filesystem.UID]
				Expect(ok).To(BeTrue())
			}

			return nil
		}, 15*time.Second, 1*time.Second).ShouldNot(gomega.HaveOccurred())
	}

	cephObjectStoreUserModify := func(shouldDelete bool) {
		objStoreUser := &cephv1.CephObjectStoreUser{}
		key := crclient.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("%s-cephobjectstoreuser", name)}
		err := client.Get(context.TODO(),
			key,
			objStoreUser,
		)
		Expect(err).To(BeNil())

		// modifying the user store name
		if shouldDelete {

			err = client.Delete(context.TODO(), objStoreUser)
			Expect(err).To(BeNil())
		} else {
			objStoreUser.Spec.Store = "fake"
			err = client.Update(context.TODO(), objStoreUser)
			Expect(err).To(BeNil())
		}
		uidMap[objStoreUser.UID] = ""
	}

	cephObjectStoreUserExpectReconcile := func(expectDelete bool) {
		objStoreUser := &cephv1.CephObjectStoreUser{}
		key := crclient.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("%s-cephobjectstoreuser", name)}

		gomega.Eventually(func() error {
			err := client.Get(context.TODO(),
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
				_, ok := uidMap[objStoreUser.UID]
				Expect(ok).To(BeTrue())
			}

			return nil
		}, 15*time.Second, 1*time.Second).ShouldNot(gomega.HaveOccurred())
	}

	cephBlockPoolModify := func(shouldDelete bool) {
		blockPool := &cephv1.CephBlockPool{}
		key := crclient.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("%s-cephblockpool", name)}
		err := client.Get(context.TODO(),
			key,
			blockPool,
		)
		Expect(err).To(BeNil())

		// modifying the failure domain
		if shouldDelete {
			err = client.Delete(context.TODO(), blockPool)
			Expect(err).To(BeNil())
		} else {
			blockPool.Spec.FailureDomain = "fake"
			err = client.Update(context.TODO(), blockPool)
			Expect(err).To(BeNil())
		}

		uidMap[blockPool.UID] = ""
	}

	cephBlockPoolExpectReconcile := func(expectDelete bool) {
		blockPool := &cephv1.CephBlockPool{}
		key := crclient.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("%s-cephblockpool", name)}

		gomega.Eventually(func() error {
			err := client.Get(context.TODO(),
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
				_, ok := uidMap[blockPool.UID]
				Expect(ok).To(BeTrue())
			}

			return nil
		}, 15*time.Second, 1*time.Second).ShouldNot(gomega.HaveOccurred())
	}

	cephObjectStoreModify := func(shouldDelete bool) {
		objStore := &cephv1.CephObjectStore{}
		key := crclient.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("%s-cephobjectstore", name)}
		err := client.Get(context.TODO(),
			key,
			objStore,
		)
		Expect(err).To(BeNil())

		// modifying the gateway instance count
		if shouldDelete {
			err = client.Delete(context.TODO(), objStore)
			Expect(err).To(BeNil())
		} else {
			objStore.Spec.Gateway.Instances = 5
			err = client.Update(context.TODO(), objStore)
			Expect(err).To(BeNil())

		}

		uidMap[objStore.UID] = ""
	}

	cephObjectStoreExpectReconcile := func(expectDelete bool) {
		objStore := &cephv1.CephObjectStore{}
		key := crclient.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("%s-cephobjectstore", name)}

		gomega.Eventually(func() error {
			err := client.Get(context.TODO(),
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
				_, ok := uidMap[objStore.UID]
				Expect(ok).To(BeTrue())
			}

			return nil
		}, 15*time.Second, 1*time.Second).ShouldNot(gomega.HaveOccurred())
	}

	storageClassModify := func(shouldDelete bool) {
		storageClass := &storagev1.StorageClass{}
		key := crclient.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("%s-ceph-rbd", name)}
		err := client.Get(context.TODO(),
			key,
			storageClass,
		)
		Expect(err).To(BeNil())

		if shouldDelete {
			err = client.Delete(context.TODO(), storageClass)
			Expect(err).To(BeNil())

		} else {
			// I couldn't find a storageClass field in the Spec that
			// the apiserver allows mutations on.
			// We'll still verify the UID doesn't change in during
			// the reconcile loop though.
		}

		uidMap[storageClass.UID] = ""
	}

	storageClassExpectReconcile := func(expectDelete bool) {
		storageClass := &storagev1.StorageClass{}
		key := crclient.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("%s-ceph-rbd", name)}

		gomega.Eventually(func() error {
			err := client.Get(context.TODO(),
				key,
				storageClass,
			)
			if err != nil {
				return err
			}

			if !expectDelete {
				_, ok := uidMap[storageClass.UID]
				Expect(ok).To(BeTrue())
			}

			return nil
		}, 15*time.Second, 1*time.Second).ShouldNot(gomega.HaveOccurred())
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
				if currentCloudPlatform != scController.PlatformAWS {
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
				if currentCloudPlatform != scController.PlatformAWS {
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
				if currentCloudPlatform != scController.PlatformAWS {
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
				if currentCloudPlatform != scController.PlatformAWS {
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
})
