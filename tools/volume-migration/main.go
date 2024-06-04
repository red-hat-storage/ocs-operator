// +kubebuilder:skip
// +kubebuilder:validation:Optional
// +optional
//
//nolint:typecheck
package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/operations"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

// TODO: Maybe import from Ceph-CSI
type ClusterConfigEntry struct {
	ClusterID       string       `json:"clusterID"`
	StorageClientID string       `json:"storageClientID"`
	Monitors        []string     `json:"monitors"`
	CephFS          *CephFSSpec  `json:"cephFS,omitempty"`
	CephRBD         *CephRBDSpec `json:"rbd,omitempty"`
}

type CephRBDSpec struct {
	RadosNamespace string `json:"radosNamespace,omitempty"`
}

type CephFSSpec struct {
	SubvolumeGroup string `json:"subvolumeGroup,omitempty"`
}

type ComponentState struct {
	Kind       string
	Phase      MigrationPhase
	WorkingPVs map[string]corev1.PersistentVolume
}

type JobState struct {
	ComponentStates map[string]ComponentState
}

const (
	MigrationPhaseAnnotation  = "volume-migration.ops.ocs.openshift.io/phase"
	MigrationSourceAnnotation = "volume-migration.ops.ocs.openshift.io/source"
	MigrationTargetAnnotation = "volume-migration.ops.ocs.openshift.io/target"
)

type MigrationPhase string

func (p MigrationPhase) ToString() string {
	return string(p)
}

const (
	MigrationPhaseRequested        MigrationPhase = "Requested" // Unused
	MigrationPhaseApproved         MigrationPhase = "Approved"  // Unused
	MigrationPhaseInitializing     MigrationPhase = "Initializing"
	MigrationPhaseDeletingVolumes  MigrationPhase = "DeletingVolumes"
	MigrationPhaseVolumesDeleted   MigrationPhase = "VolumesDeleted"
	MigrationPhaseDataMigrating    MigrationPhase = "DataMigrating"
	MigrationPhaseDataMigrated     MigrationPhase = "DataMigrated"
	MigrationPhaseRestoringVolumes MigrationPhase = "RestoringVolumes"
	MigrationPhaseVolumesRestored  MigrationPhase = "VolumesRestored"
	MigrationPhaseCompleted        MigrationPhase = "Completed"
)

var (
	DryRun     bool
	DryRunOpts = []string{}

	StateFilePath string
	State         JobState

	OpMode string
	Image  string

	StorageRequestName string
	ClusterID          string

	KubeConfig  string
	KubeContext string
	Namespace   string

	ClientSets     *Clientsets
	AllPVs         *corev1.PersistentVolumeList
	StorageClass   *storagev1.StorageClass
	StorageRequest *ocsv1alpha1.StorageRequest
)

// RootCmd represents the root cobra command
var RootCmd = &cobra.Command{
	Use: "volume-migration",
	Run: func(cmd *cobra.Command, args []string) {
		Do(cmd.Context())
	},
}

func init() {
}

var dumpCmd = &cobra.Command{
	Use:   "dump-yaml",
	Short: "Dump YAML manifests for operation Job(s)",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("---")
		volMvJob, _ := operations.GetJob("volume-migration", OpMode)
		volMvJob.Spec.Template.Spec.Containers[0].Args = append(volMvJob.Spec.Template.Spec.Containers[0].Args, "--dry-run")
		volMvJob.Spec.Template.Spec.Containers[0].Image = Image

		envVars := []corev1.EnvVar{}
		if OpMode == "converged" || OpMode == "provider" {
			if StorageRequestName == "" {
				StorageRequestName = os.Getenv("STORAGE_REQUEST")
			}
			envVars = append(envVars, corev1.EnvVar{
				Name:  "STORAGE_REQUEST",
				Value: StorageRequestName,
			})

			volMvJob.Spec.Template.Spec.InitContainers[0].Image = Image
		}

		if OpMode == "converged" || OpMode == "consumer" {
			if ClusterID == "" {
				ClusterID = os.Getenv("CLUSTER_ID")
			}
			envVars = append(envVars, corev1.EnvVar{
				Name:  "CLUSTER_ID",
				Value: ClusterID,
			})
		}

		volMvJob.Spec.Template.Spec.Containers[0].Env = append(volMvJob.Spec.Template.Spec.Containers[0].Env, envVars...)

		volMvManifest, _ := yaml.Marshal(volMvJob)
		fmt.Print(string(volMvManifest))
	},
}

func init() {
	RootCmd.AddCommand(dumpCmd)
	RootCmd.PersistentFlags().StringVar(&KubeConfig, "kubeconfig", "", "kubeconfig path")
	RootCmd.PersistentFlags().StringVar(&KubeContext, "context", "", "kubecontext to use")
	RootCmd.PersistentFlags().BoolVar(&DryRun, "dry-run", false, "no commitments")
	RootCmd.PersistentFlags().StringVarP(&StateFilePath, "state-file", "f", "state.json", "path to file where program will store state")
	RootCmd.PersistentFlags().StringVarP(&OpMode, "mode", "m", "converged", "'converged', 'provider', or 'consumer'")
	RootCmd.PersistentFlags().StringVarP(&Image, "image", "i", "quay.io/ocs-dev/ocs-volume-migration:latest", "container image for volume migration Job")
	RootCmd.PersistentFlags().StringVar(&StorageRequestName, "storage-request", "", "name of StorageRequest to migrate")
	RootCmd.PersistentFlags().StringVar(&ClusterID, "cluster-id", "", "StorageClass ClusterID of the volumes to be migrated")
	RootCmd.PersistentFlags()
}

func main() {
	err := RootCmd.Execute()
	if err != nil {
		klog.Fatal(err)
	}
}

var StateConfigMapName = "ocs-operator-volume-migration-state"

func LoadState(ctx context.Context) error {
	var err error

	klog.Info("Initializing operation state...")

	cmClient := ClientSets.Kube.CoreV1().ConfigMaps(Namespace)

	cm, err := cmClient.Get(ctx, StateConfigMapName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		stateJSON, err := json.Marshal(State)
		if err != nil {
			return err
		}
		cm = &corev1.ConfigMap{
			BinaryData: map[string][]byte{"State": stateJSON},
		}
	} else if err != nil {
		return err
	}

	err = json.Unmarshal(cm.BinaryData["State"], &State)
	if err != nil {
		return err
	}

	return nil
}

// SaveState updates the current state and saves it to the state ConfigMap
func SaveState(ctx context.Context) error {
	var err error

	klog.Info("Saving current state...")

	// If StorageClass is defined, read or update the StorageClass migration phase annotation
	if StorageClass != nil {
		workingState, ok := State.ComponentStates[StorageClass.Name]
		if !ok {
			workingState = ComponentState{
				Kind:       "StorageClass",
				Phase:      MigrationPhaseInitializing,
				WorkingPVs: map[string]corev1.PersistentVolume{},
			}
			State.ComponentStates[StorageClass.Name] = workingState
		}

		if phase, ok := StorageClass.ObjectMeta.Annotations[MigrationPhaseAnnotation]; !ok || phase == "" {
			StorageClass.ObjectMeta.Annotations[MigrationPhaseAnnotation] = State.ComponentStates[StorageClass.Name].Phase.ToString()
			_, err = ClientSets.Kube.StorageV1().StorageClasses().Update(ctx, StorageClass, metav1.UpdateOptions{DryRun: DryRunOpts})
			if err != nil {
				klog.Fatal(err)
			}
		} else if phase != workingState.Phase.ToString() {
			workingState.Phase = MigrationPhase(phase)
			State.ComponentStates[StorageClass.Name] = workingState
		}
	}

	// If StorageRequest is defined, read or update the StorageRequest migration phase annotation
	if StorageRequest != nil {
		workingState, ok := State.ComponentStates[StorageRequest.Name]
		if !ok {
			workingState = ComponentState{
				Kind:  "StorageRequest",
				Phase: MigrationPhaseInitializing,
			}
			State.ComponentStates[StorageRequest.Name] = workingState
		}

		if phase, ok := StorageRequest.ObjectMeta.Annotations[MigrationPhaseAnnotation]; !ok || phase != "" {
			StorageRequest.ObjectMeta.Annotations[MigrationPhaseAnnotation] = workingState.Phase.ToString()
			srResult := &ocsv1alpha1.StorageRequest{}
			err = ClientSets.OcsV1alpha1.Post().
				Resource("storagerequests").
				Body(StorageRequest).
				Do(ctx).
				Into(srResult)
			if err != nil {
				klog.Fatal(err)
			}
		} else if phase != workingState.Phase.ToString() {
			workingState.Phase = MigrationPhase(phase)
			State.ComponentStates[StorageClass.Name] = workingState
		}
	}

	// write current state to ConfigMap
	cmClient := ClientSets.Kube.CoreV1().ConfigMaps(Namespace)

	stateJSON, err := json.Marshal(State)
	if err != nil {
		return err
	}

	cm := &corev1.ConfigMap{
		BinaryData: map[string][]byte{"State": stateJSON},
	}

	_, err = cmClient.Update(ctx, cm, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		_, err = cmClient.Create(ctx, cm, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}

// Do does the main thing
func Do(ctx context.Context) {
	var err error

	klog.Info("Doing the thing!")
	if DryRun {
		klog.Info("...but not really.")
		DryRunOpts = append(DryRunOpts, "All")
	}

	Namespace = os.Getenv("POD_NAMESPACE")
	if Namespace == "" {
		klog.Fatal("Env var POD_NAMESPACE not set!")
	}

	// Get k8s clientsets
	ClientSets = GetClientsets()

	// Load or initialize job state
	err = LoadState(ctx)
	if err != nil {
		klog.Fatal(err)
	}

	switch OpMode {
	case "converged":
		FindStorageClass(ctx)
		DeletePVs(ctx)
		MigrateRbdVolumes(ctx)
		RestorePVs(ctx)
	case "provider":
		MigrateRbdVolumes(ctx)
	case "consumer":
		FindStorageClass(ctx)
		DeletePVs(ctx)
		RestorePVs(ctx)
	}

	klog.Info("Done! Good luck out there.")
}

// FindStorageClass looks for a StorageClass with a matching ClusterID and
// either reads or sets the phase of the operation
func FindStorageClass(ctx context.Context) {
	if ClusterID == "" {
		if ClusterID = os.Getenv("CLUSTER_ID"); ClusterID == "" {
			klog.Fatal("CLUSTER_ID not specified")
		}
	}

	// Get the list of StorageClasses and check for a CephCSI class
	// matching the desired clusterID
	storageClassList, err := ClientSets.Kube.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		klog.Fatal(err)
	}
	for i, storageClass := range storageClassList.Items {
		if id, ok := storageClass.Parameters["clusterID"]; ok && id == ClusterID {
			StorageClass = &storageClassList.Items[i]
		}
	}
	if StorageClass == nil {
		klog.Fatal("No StorageClass found, doing no such thing!")
	}

	// list and store all PVs in the cluster, for later parsing
	AllPVs, err = ClientSets.Kube.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		klog.Fatal(err)
	}

	klog.Infof("Migrating volumes for StorageClass %q", StorageClass.Name)

	err = SaveState(ctx)
	if err != nil {
		klog.Fatal(err)
	}
}

func DeletePVs(ctx context.Context) {
	if StorageClass == nil {
		klog.Info("none storageclass")
		return
	}
	workingState, ok := State.ComponentStates[StorageClass.Name]
	if !ok {
		klog.Info("none storageclass state")
		return
	}

	switch workingState.Phase {
	case MigrationPhaseInitializing:
		workingState.Phase = MigrationPhaseDeletingVolumes
		fallthrough
	case MigrationPhaseDeletingVolumes:
		for _, pv := range AllPVs.Items {
			if pv.Spec.StorageClassName == StorageClass.Name {
				workingState.WorkingPVs[pv.Name] = pv
			}
		}

		pvs := workingState.WorkingPVs
		if len(pvs) == 0 {
			klog.Infof("no PVs found for StorageClass %q", StorageClass.Name)
		} else {
			remainingVolumes, err := deleteVolumes(ctx, StorageClass, &workingState)
			if err != nil {
				klog.Fatal(err)
			}
			if len(remainingVolumes) != 0 {
				klog.Fatal("multiple volumes left, restart")
			}
		}

		workingState.Phase = MigrationPhaseVolumesDeleted

		err := SaveState(ctx)
		if err != nil {
			klog.Fatal(err)
		}
	default:
		return
	}
}

func RestorePVs(ctx context.Context) {
	workingState := State.ComponentStates[StorageClass.Name]

	_ = SaveState(ctx)

	if workingState.Phase == MigrationPhaseDataMigrated {
		workingState.Phase = MigrationPhaseRestoringVolumes
		err := restoreVolumes(ctx, StorageClass, &workingState)
		if err != nil {
			klog.Fatal(err)
		}
		workingState.Phase = MigrationPhaseVolumesRestored
	}
}

// MigrateRbdVolumes does the main thing for provider mode
func MigrateRbdVolumes(ctx context.Context) {
	var err error

	err = LoadState(ctx)
	if err != nil {
		klog.Fatal(err)
	}

	srName := os.Getenv("STORAGE_REQUEST")
	StorageRequest = &ocsv1alpha1.StorageRequest{}
	err = ClientSets.OcsV1alpha1.Get().
		Name(srName).
		Namespace(Namespace).
		Do(ctx).
		Into(StorageRequest)
	if err != nil {
		klog.Error(err)
	}

	workingState, ok := State.ComponentStates[srName]
	if !ok {
		err = SaveState(ctx)
		if err != nil {
			klog.Fatal(err)
		}

		workingState = State.ComponentStates[srName]
	}

	defer func() {
		err = SaveState(ctx)
		if err != nil {
			klog.Fatal(err)
		}
	}()

	klog.Infof("Migrating RBD volumes for StorageRequest %s", srName)

	/* TODO: actually use pending-ops?
	volOpFound := false
	var ops []string
	if pendingOps, ok := scr.Annotations[operations.PendingOperationsAnnotation]; ok {
		ops = strings.Split(pendingOps, ",")
		for _, op := range ops {
			if op == "volume-migration" {
				volOpFound = true
				continue
			}
		}
	}
	if !volOpFound {
		klog.Fatal("scr has no op!")
	}
	*/

	for workingState.Phase != MigrationPhaseDataMigrated {
		switch workingState.Phase {
		case MigrationPhaseVolumesDeleted:
			// STRETCH TODO: maybe have some provider API call initiate an approval
			migrationSource := StorageRequest.Annotations[MigrationSourceAnnotation]
			migrationTarget := StorageRequest.Annotations[MigrationTargetAnnotation]

			if migrationSource == "" {
				// NOTE: 4.15 still used Status to track ownership of
				// CephBlockPools, so we need to rely on that to auto-detect
				// the migration source.
				for _, res := range StorageRequest.Status.CephResources {
					if res.Kind == "CephBlockPool" {
						migrationSource = res.Name
						break
					}
				}
				if migrationSource == "" {
					klog.Fatal("no migration source found")
				}
			}
			if migrationTarget == "" {
				md5Sum := md5.Sum([]byte(StorageRequest.Name))
				migrationTarget = fmt.Sprintf("cephradosnamespace-%s", hex.EncodeToString(md5Sum[:16]))
			}

			StorageRequest.Annotations[MigrationSourceAnnotation] = migrationSource
			StorageRequest.Annotations[MigrationTargetAnnotation] = migrationTarget

			workingState.Phase = MigrationPhaseDataMigrating
		case MigrationPhaseDataMigrating:
			migrationSource := StorageRequest.Annotations[MigrationSourceAnnotation]
			migrationTarget := StorageRequest.Annotations[MigrationTargetAnnotation]

			statusChan := make(chan string)
			go func() {
				err = PhaseDataMigration(migrationSource, migrationTarget, statusChan)
				if err != nil {
					klog.Error("ope!")
				} else {
					workingState.Phase = MigrationPhaseDataMigrated
				}
			}()

			for status := range statusChan {
				klog.Info(status)
			}
		default:
			klog.Infof("volume migration in progress for StorageRequest %q: %q", StorageRequest.Name, workingState.Phase)
			break
		}

		/* TODO: Never update status?
		StorageRequest.ObjectMeta.ResourceVersion = scrResult.ResourceVersion
		err = ClientSets.OcsV1alpha1.Put().
			Name(StorageRequest.Name).
			Resource("storagerequests").
			SubResource("status").
			Body(StorageRequest).
			Do(ctx).
			Into(scrResult)
		if err != nil && !errors.IsAlreadyExists(err) {
			klog.Fatal(err)
		}
		*/

		time.Sleep(time.Second * 5)
	}
}

func deleteVolumes(ctx context.Context, sc *storagev1.StorageClass, workingState *ComponentState) (map[string]corev1.PersistentVolume, error) {
	pvClient := ClientSets.Kube.CoreV1().PersistentVolumes()
	pvs := workingState.WorkingPVs
	pvsDeleting := true

	for pvsDeleting {
		pvsDeleting = false
		for pvName := range pvs {
			pv, err := pvClient.Get(ctx, pvName, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				delete(pvs, pvName)
				continue
			}
			if !DryRun {
				pvsDeleting = true
			}
			if !pv.ObjectMeta.DeletionTimestamp.IsZero() {
				klog.Infof("Volume %q for StorageClass %q is deleting...", pvName, sc.Name)
				continue
			}

			// Look for Pods referencing a PVC that is bound to the PV
			if inUse, err := volumeInUse(ctx, pv); err != nil || inUse {
				if err != nil {
					klog.Error(err)
				}
				continue
			}
			klog.Infof("No Pods found using PV %q, let's go!", pvName)

			// Edit and delete the PV without deleting the underlying RBD volume
			klog.Infof("Deleting volume %q for StorageClass %q", pvName, sc.Name)
			pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
			pv.ObjectMeta.Finalizers = nil

			updatedPv, err := pvClient.Update(ctx, pv,
				metav1.UpdateOptions{DryRun: DryRunOpts})
			if err != nil {
				klog.Error(err)
				continue
			}
			pvs[pvName] = *updatedPv

			err = pvClient.Delete(ctx, pvName,
				metav1.DeleteOptions{DryRun: DryRunOpts})
			if err != nil {
				klog.Error(err)
				continue
			}
		}
	}

	return pvs, nil
}

func volumeInUse(ctx context.Context, pv *corev1.PersistentVolume) (bool, error) {
	coreClient := ClientSets.Kube.CoreV1()
	foundPods := false

	pvc, err := coreClient.PersistentVolumeClaims(pv.Spec.ClaimRef.Namespace).Get(ctx, pv.Spec.ClaimRef.Name, metav1.GetOptions{})
	if err != nil {
		klog.Error(err)
		return foundPods, err
	}
	klog.Infof("found PVC bound to volume %q: %q", pv.Name, pvc.Name)

	pods, err := coreClient.Pods(pv.Spec.ClaimRef.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		klog.Error(err)
		return foundPods, err
	}

	for _, pod := range pods.Items {
		for _, volume := range pod.Spec.Volumes {
			if volume.VolumeSource.PersistentVolumeClaim != nil && volume.VolumeSource.PersistentVolumeClaim.ClaimName == pvc.Name {
				klog.Warningf("found Pod using PVC %q: %q", pvc.Name, pod.Name)
				foundPods = true
			}
		}
	}
	if foundPods {
		klog.Errorf("found Pods using PVC %q, will not migrate", pvc.Name)
		return foundPods, err
	}

	return foundPods, nil
}

func restoreVolumes(ctx context.Context, sc *storagev1.StorageClass, workingState *ComponentState) error {
	pvClient := ClientSets.Kube.CoreV1().PersistentVolumes()
	pvs := workingState.WorkingPVs
	newSc, err := ClientSets.Kube.StorageV1().StorageClasses().Get(ctx, sc.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	clusterID := newSc.Parameters["clusterID"]
	cm, err := ClientSets.Kube.CoreV1().ConfigMaps(Namespace).Get(ctx, "ceph-csi-configs", metav1.GetOptions{})
	if err != nil {
		return err
	}
	csiConfigs := []ClusterConfigEntry{}
	err = json.Unmarshal([]byte(cm.Data["config.json"]), &csiConfigs)
	if err != nil {
		return err
	}

	newRadosNamespace := ""
	for _, config := range csiConfigs {
		if config.ClusterID == clusterID && config.CephRBD != nil && config.CephRBD.RadosNamespace != "" {
			newRadosNamespace = config.CephRBD.RadosNamespace
			break
		}
	}

	for pvName, pv := range pvs {
		actualPv, err := pvClient.Get(ctx, pvName, metav1.GetOptions{})
		if !errors.IsNotFound(err) {
			if phase, ok := actualPv.Annotations["migration-state"]; ok && phase == "migrated" {
				delete(pvs, pvName)
			}
			continue
		}

		if newRadosNamespace != "" {
			pv.Spec.CSI.VolumeAttributes["radosNamespace"] = newRadosNamespace
		}
		pv.Spec.CSI.VolumeAttributes["clusterID"] = clusterID
		pv.Spec.CSI.VolumeAttributes["staticVolume"] = "true"
		pv.Spec.CSI.VolumeHandle = pv.Spec.CSI.VolumeAttributes["imageName"]
		pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimDelete

		pv.ObjectMeta.Annotations["storage-migration.odf.openshift.io/state"] = "migrated"

		createdPv, err := pvClient.Create(ctx, &pv,
			metav1.CreateOptions{DryRun: DryRunOpts})
		if err != nil {
			klog.Error(err)
			if createdPv.Annotations["migration-state"] != "migrated" {
				klog.Warningf("volumes remain, re-run job")
			}
			continue
		}

		delete(pvs, pvName)
	}

	workingState.WorkingPVs = pvs
	return nil
}
