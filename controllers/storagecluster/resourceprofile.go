package storagecluster

import (
	"errors"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/v4/api/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	corev1 "k8s.io/api/core/v1"
)

const (
	resourceProfileChangeApplyingMessage = "New resource profile is being applied"
	resourceProfileChangeFailedMessage   = "New resource profile failed to apply, please revert to the last working resource profile"
)

var (
	errResourceProfileChangeApplying = errors.New(resourceProfileChangeApplyingMessage)
	errResourceProfileChangeFailed   = errors.New(resourceProfileChangeFailedMessage)
)

func (r *StorageClusterReconciler) ensureResourceProfileChangeApplied(sc *ocsv1.StorageCluster) error {
	currentResourceProfile := sc.Spec.ResourceProfile
	lastAppliedResourceProfile := sc.Status.LastAppliedResourceProfile
	r.Log.Info("Applying new resource profile", "current", currentResourceProfile, "last", lastAppliedResourceProfile)

	// If any of the pods with the current resource profile label are in pending state for more than 2 minutes, then we assume that the resource profile change has failed as the pods are stuck in pending state
	podList, err := util.GetPodsWithLabels(r.ctx, r.Client, sc.Namespace, map[string]string{defaults.ODFResourceProfileKey: currentResourceProfile})
	if err != nil {
		return err
	}
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodPending {
			if time.Since(pod.CreationTimestamp.Time) < 2*time.Minute {
				return errResourceProfileChangeApplying
			}
			r.Log.Error(errResourceProfileChangeFailed, "Pod is stuck in pending state", "pod", pod.Name)
			return errResourceProfileChangeFailed
		}
	}

	// Verify if expected number of mgr pods with the current resource profile label are running
	if err := r.verifyDaemonWithResourceProfile(sc.Namespace, map[string]string{"app": "rook-ceph-mgr", defaults.ODFResourceProfileKey: currentResourceProfile}, getMgrCount(sc)); err != nil {
		return err
	}

	// Verify if expected number of mon pods with the current resource profile label are running
	if err := r.verifyDaemonWithResourceProfile(sc.Namespace, map[string]string{"app": "rook-ceph-mon", defaults.ODFResourceProfileKey: currentResourceProfile}, getMonCount(sc)); err != nil {
		return err
	}

	// Verify if expected number of osd pods with the current resource profile label are running
	if err := r.verifyDaemonWithResourceProfile(sc.Namespace, map[string]string{"app": "rook-ceph-osd", defaults.ODFResourceProfileKey: currentResourceProfile}, r.getOsdCount(sc)); err != nil {
		return err
	}

	// Verify if expected number of mds pods with the current resource profile label are running
	if err := r.verifyDaemonWithResourceProfile(sc.Namespace, map[string]string{"app": "rook-ceph-mds", defaults.ODFResourceProfileKey: currentResourceProfile}, 2*getActiveMetadataServers(sc)); err != nil {
		return err
	}

	// If rgw is not skipped, Verify if expected number of rgw pods with the current resource profile label are running
	skiprgw, err := r.PlatformsShouldSkipObjectStore()
	if err != nil {
		return err
	}
	if !skiprgw {
		if err := r.verifyDaemonWithResourceProfile(sc.Namespace, map[string]string{"app": "rook-ceph-rgw", defaults.ODFResourceProfileKey: currentResourceProfile}, getCephObjectStoreGatewayInstances(sc)); err != nil {
			return err
		}
	}

	// If we are here, then all the ceph daemons have the correct count of pods with the new resource profile
	// New resource profile has been applied successfully
	sc.Status.LastAppliedResourceProfile = currentResourceProfile
	if err = r.Client.Status().Update(r.ctx, sc); err != nil {
		r.Log.Error(err, "Error updating status after resource profile change")
		return err
	}
	r.Log.Info("Resource profile change applied successfully", "current", currentResourceProfile, "last", lastAppliedResourceProfile)
	return nil
}

// verifyDaemonsWithResourceProfile verifies if a ceph daemon has the expected number of pods running with the given resource profile label
func (r *StorageClusterReconciler) verifyDaemonWithResourceProfile(namespace string, labelSelector map[string]string, count int) error {
	podList, err := util.GetPodsWithLabels(r.ctx, r.Client, namespace, labelSelector)
	if err != nil {
		return err
	}
	if util.GetCountOfRunningPods(podList) != count {
		r.Log.Info("pod count mismatch", "app", labelSelector["app"], "expected", count, "actual", len(podList.Items))
		return errResourceProfileChangeApplying
	}
	return nil
}
