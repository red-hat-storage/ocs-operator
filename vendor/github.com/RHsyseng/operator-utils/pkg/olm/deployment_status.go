package olm

import (
	"fmt"
	oappsv1 "github.com/openshift/api/apps/v1"
	appsv1 "k8s.io/api/apps/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("olm")

func GetDaemonSetStatus(dcs []appsv1.DaemonSet) DeploymentStatus {
	return getDeploymentStatus(deploymentsWrapper{
		countFunc: func() int {
			return len(dcs)
		},
		nameFunc: func(i int) string {
			return dcs[i].Name
		},
		requestedReplicasFunc: func(i int) int32 {
			//DaemonSet means an implicit replica count request of one per node, return >0:
			return 1
		},
		targetReplicasFunc: func(i int) int32 {
			return dcs[i].Status.DesiredNumberScheduled
		},
		readyReplicasFunc: func(i int) int32 {
			return dcs[i].Status.NumberReady
		},
	})
}

func GetDeploymentStatus(dcs []appsv1.Deployment) DeploymentStatus {
	return getDeploymentStatus(deploymentsWrapper{
		countFunc: func() int {
			return len(dcs)
		},
		nameFunc: func(i int) string {
			return dcs[i].Name
		},
		requestedReplicasFunc: func(i int) int32 {
			return getInt32(dcs[i].Spec.Replicas)
		},
		targetReplicasFunc: func(i int) int32 {
			return dcs[i].Status.Replicas
		},
		readyReplicasFunc: func(i int) int32 {
			return dcs[i].Status.ReadyReplicas
		},
	})
}

func GetDeploymentConfigStatus(dcs []oappsv1.DeploymentConfig) DeploymentStatus {
	return getDeploymentStatus(deploymentsWrapper{
		countFunc: func() int {
			return len(dcs)
		},
		nameFunc: func(i int) string {
			return dcs[i].Name
		},
		requestedReplicasFunc: func(i int) int32 {
			return dcs[i].Spec.Replicas
		},
		targetReplicasFunc: func(i int) int32 {
			return dcs[i].Status.Replicas
		},
		readyReplicasFunc: func(i int) int32 {
			return dcs[i].Status.ReadyReplicas
		},
	})
}

func getDeploymentStatus(obj deployments) DeploymentStatus {
	var ready, starting, stopped []string
	for i := 0; i < obj.count(); i++ {
		if obj.requestedReplicas(i) == 0 {
			stopped = append(stopped, obj.name(i))
		} else if obj.targetReplicas(i) == 0 {
			stopped = append(stopped, obj.name(i))
		} else if obj.readyReplicas(i) < obj.targetReplicas(i) {
			starting = append(starting, obj.name(i))
		} else {
			ready = append(ready, obj.name(i))
		}
	}
	log.Info("Found deployments with status ", "stopped", stopped, "starting", starting, "ready", ready)
	return DeploymentStatus{
		Stopped:  stopped,
		Starting: starting,
		Ready:    ready,
	}

}

func GetSingleDaemonSetStatus(ds appsv1.DaemonSet) DeploymentStatus {
	return getSingleDeploymentStatus(ds.Name, 1, ds.Status.DesiredNumberScheduled, ds.Status.NumberReady)
}

func GetSingleDeploymentStatus(dc appsv1.Deployment) DeploymentStatus {
	return getSingleDeploymentStatus(dc.Name, getInt32(dc.Spec.Replicas), dc.Status.Replicas, dc.Status.ReadyReplicas)
}

func GetSingleStatefulSetStatus(ss appsv1.StatefulSet) DeploymentStatus {
	return getSingleDeploymentStatus(ss.Name, getInt32(ss.Spec.Replicas), ss.Status.Replicas, ss.Status.ReadyReplicas)
}

func getInt32(pointer *int32) int32 {
	if pointer == nil {
		return 0
	} else {
		return *pointer
	}

}
func getSingleDeploymentStatus(name string, requestedCount int32, targetCount int32, readyCount int32) DeploymentStatus {
	var ready, starting, stopped []string
	if requestedCount == 0 || targetCount == 0 {
		stopped = append(stopped, name)
	} else {
		for i := int32(0); i < targetCount; i++ {
			instanceName := fmt.Sprintf("%s-%d", name, i+1)
			if i < readyCount {
				ready = append(ready, instanceName)
			} else {
				starting = append(starting, instanceName)
			}
		}
	}
	log.Info("Found deployments with status ", "stopped", stopped, "starting", starting, "ready", ready)
	return DeploymentStatus{
		Stopped:  stopped,
		Starting: starting,
		Ready:    ready,
	}

}
