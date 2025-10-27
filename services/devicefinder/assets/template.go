package assets

import (
	"strings"
)

const (
	// DeviceFinderDaemonSetTemplate is the YAML template for the device finder daemonset
	DeviceFinderDaemonSetTemplate = `apiVersion: apps/v1
kind: DaemonSet
metadata:
  labels:
    app: devicefinder-discovery
  name: devicefinder-discovery
  namespace: ${OBJECT_NAMESPACE}
spec:
  selector:
    matchLabels:
      app: devicefinder-discovery
  template:
    metadata:
      annotations:
        target.workload.openshift.io/management: '{"effect": "PreferredDuringScheduling"}'
      labels:
        app: devicefinder-discovery
    spec:
      containers:
      - args:
        - discover
        env:
        - name: MY_NODE_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: spec.nodeName
        - name: WATCH_NAMESPACE
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.namespace
        - name: POD_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.name
        image: ${CONTAINER_IMAGE}
        imagePullPolicy: Always
        name: devicefinder-discovery
        securityContext:
          privileged: true
        resources:
          requests:
            memory: 50Mi
            cpu: 10m
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: FallbackToLogsOnError
        volumeMounts:
        - mountPath: /dev
          mountPropagation: HostToContainer
          name: device-dir
        - mountPath: /run/udev
          mountPropagation: HostToContainer
          name: run-udev
      priorityClassName: ${PRIORITY_CLASS_NAME}
      serviceAccountName: ocs-operator-controller-manager
      volumes:
      - hostPath:
          path: /dev
          type: Directory
        name: device-dir
      - hostPath:
          path: /run/udev
          type: ""
        name: run-udev
  updateStrategy:
    rollingUpdate:
      maxSurge: 0
      maxUnavailable: 10%
    type: RollingUpdate`
)

// ReadFileAndReplace reads the template, replaces a set of strings, and
// returns the resulting contents as a byte array. The pairs should be
// provided as key/value pairs using an array of strings, i.e.:
//
//	pairs := []string{
//		"${KEY_1}", "val_1",
//		"${KEY_2}", "val_2",
//	}
func ReadFileAndReplace(template string, pairs []string) ([]byte, error) {
	replacer := strings.NewReplacer(pairs...)
	transformedString := replacer.Replace(template)
	return []byte(transformedString), nil
}
