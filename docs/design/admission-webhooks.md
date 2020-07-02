## Background

This proposal is to add support for admission webhooks for validation and mutation for API requests on storagecluster CRs

An admission webhook are HTTP callbacks that intercept and pre-process requests to the Kubernetes API server for a specific resource, prior to persistence of the resource, but after the request is authenticated and authorized

There are two type of admission webhooks:   
 
**MutatingAdmissionWebhook** - During the admission process, the mutating admission webhooks can perform tasks on the resource, such as injecting affinity labels or filling default values

**ValidatingAdmissionWebhook** - At the end of the admission process, the validating admission plug-in can be used to make sure an object is configured properly, for example ensuring affinity labels are as expected. If the validation passes, the api server will persist the changes to the resource, otherwise the api server will fail the request and return an error to the client

Admission webhooks are implemented  as part of a specialized web server that is deployed on the cluster. 
In order to expose the web server and the webhooks to the API server a MutatingWebhookConfiguration / ValidatingWebhookConfiguration with proper configuration are also needed to be installed on the cluster

For more information on admission webhooks (admission plugins) please follow the links below:

[General information about admission webhooks](https://docs.openshift.com/container-platform/4.5/architecture/admission-plug-ins.html)

[API reference for the MutatingWebhookConfiguration resour  ce](
https://docs.openshift.com/container-platform/4.4/rest_api/extension_apis/mutatingwebhookconfiguration-admissionregistration-k8s-io-v1.html)


[API reference for the ValidatingWebhookConfiguration resource](https://docs.openshift.com/container-platform/4.4/rest_api/extension_apis/validatingwebhookconfiguration-admissionregistration-k8s-io-v1.html)

## User Scenario

The following is a list of use cases suggested to be solved using admission webhooks:

1. Ensuring a finalizer exists on the storagecluster CR.  
1. Prevent a request from changing spec.multiCloudGateway.reconcileStrategy from “Managed” to  any other value.  
1. Ensuring that replica count cannot be configured after deviceSet is created.

## Requirements

1. Webhooks web server which will implement the validation and mutation operations
2. ValidatingWebhookConfig / MutatingWebhookConfig installed in the cluster 
3. TLS certificates to be used by the web server to allow authenticated and secure communication between the API server and the webhooks web server. 


## Implementation 

**Code**

The web server will implement 2 specific paths:
/mutate - Exposes the implementation of the mutation hooks for storagecluster CRs
/validate - Exposes the implementation of the validation hooks for  (Validating Webhook)storagecluster CRs
All other paths should return 404.

**Image**  

We suggest implementing the web server as a new binary that will be included in the operator image and provide a new entry point that will allow a pod mounting the image to run the web server as a standalone process.

This will save us from building, managing, uploading and providing another image as part of the OCS cluster deployment. 

**Deployment method**  

We suggest relying on OLM to deploy and manage the webhooks server as described here: 
https://docs.openshift.com/container-platform/4.5/operators/user/olm-webhooks.html

This means including webhookdefinitions section and a new deployment to run the web server to the ocs ClusterServiceVersion resource.

Choosing to use OLM will bring with it the following benefits:
OLM will create and ensure  MutatingWebhookConfiguration and ValidatingWebhookConfiguration on the cluster
OLM will create and ensure the web server deployment and pods
OLM will take care of mounting the.cert and .key files (at /apiserver.local.config/certificates/) that should be used to provide authenticated and secure communication with the API server by providing both files on the web server pod
OLM will take care of exposing and routing the web server pod to be used by the API server.
