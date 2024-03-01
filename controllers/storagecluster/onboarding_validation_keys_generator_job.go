package storagecluster

import (
	"fmt"
	"os"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
)

const (
	onboardingValidationKeysGeneratorImage   = "ONBOARDING_VALIDATION_KEYS_GENERATOR_IMAGE"
	onboardingValidationKeysGeneratorJobName = "onboarding-validation-keys-generator"
	onboardingValidationPublicKeySecretName  = "onboarding-ticket-key"
)

var onboardingJobSpec = batchv1.JobSpec{
	// Eligible to delete automatically when job finishes
	TTLSecondsAfterFinished: ptr.To(int32(0)),
	Template: corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			RestartPolicy:      corev1.RestartPolicyOnFailure,
			ServiceAccountName: onboardingValidationKeysGeneratorJobName,
			Containers: []corev1.Container{
				{
					Name:    onboardingValidationKeysGeneratorJobName,
					Image:   os.Getenv(onboardingValidationKeysGeneratorImage),
					Command: []string{"/usr/local/bin/onboarding-validation-keys-gen"},
					Env: []corev1.EnvVar{
						{
							Name:  util.OperatorNamespaceEnvVar,
							Value: os.Getenv(util.OperatorNamespaceEnvVar),
						},
					},
				},
			},
		},
	},
}

type onboardingValidationKeysGeneratorJob struct{}

var _ resourceManager = &onboardingValidationKeysGeneratorJob{}

func (o *onboardingValidationKeysGeneratorJob) ensureCreated(r *StorageClusterReconciler, storagecluster *ocsv1.StorageCluster) (reconcile.Result, error) {

	if !storagecluster.Spec.AllowRemoteStorageConsumers {
		r.Log.Info("Spec.AllowRemoteStorageConsumers is disabled, skipping onboarding validation key generator job creation")
		return reconcile.Result{}, nil
	}

	if res, err := o.createJob(r, storagecluster.Namespace); err != nil {
		return reconcile.Result{}, err
	} else if !res.IsZero() {
		return res, nil
	}

	return reconcile.Result{}, nil
}

func (o *onboardingValidationKeysGeneratorJob) createJob(r *StorageClusterReconciler, namespace string) (reconcile.Result, error) {
	var err error
	r.Log.Info("Spec.AllowRemoteStorageConsumers is enabled. Creating Onboarding validation key generator job")
	if os.Getenv(onboardingValidationKeysGeneratorImage) == "" {
		err = fmt.Errorf("OnboardingSecretGeneratorImage env var is not set")
		r.Log.Error(err, "No value set for env variable")
		return reconcile.Result{}, err
	}
	publicKeySecret := &corev1.Secret{}
	publicKeySecret.Name = onboardingValidationPublicKeySecretName
	publicKeySecret.Namespace = namespace
	actualSecret := &corev1.Secret{}
	// Creating the job only if public key is not found
	err = r.Client.Get(r.ctx, client.ObjectKeyFromObject(publicKeySecret), actualSecret)
	if kerrors.IsNotFound(err) {
		onboardingSecretGeneratorJob := &batchv1.Job{}
		onboardingSecretGeneratorJob.Name = onboardingValidationKeysGeneratorJobName
		onboardingSecretGeneratorJob.Namespace = namespace
		onboardingSecretGeneratorJob.Spec = *onboardingJobSpec.DeepCopy()
		err = r.Client.Create(r.ctx, onboardingSecretGeneratorJob)
		if err != nil && !kerrors.IsAlreadyExists(err) {
			r.Log.Error(err, "failed to create onboarding validation key generator job")
			return reconcile.Result{}, err
		}

	}
	return reconcile.Result{}, nil
}

func (o *onboardingValidationKeysGeneratorJob) ensureDeleted(r *StorageClusterReconciler, storagecluster *ocsv1.StorageCluster) (reconcile.Result, error) {
	onboardingSecretGeneratorJob := &batchv1.Job{}
	onboardingSecretGeneratorJob.Name = onboardingValidationKeysGeneratorJobName
	onboardingSecretGeneratorJob.Namespace = storagecluster.Namespace
	if err := r.Client.Delete(r.ctx, onboardingSecretGeneratorJob); err != nil && !kerrors.IsNotFound(err) {
		r.Log.Info("Failed to delete secret %s: %v", onboardingSecretGeneratorJob.Name, err)
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}
