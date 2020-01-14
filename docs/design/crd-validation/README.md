## Background

This proposal is to provide validation for OCS Operator CRs. Currently, the user Creates/Updates the CR, the reconciliation might fail when there is an error in the user input. It will be a better user experience if we validate the request right after user submits the changes and before the CR gets written into etcd.

## User Senarios

Listing out some use cases, where validators will be helpful,

a. **Expanding the cluster / Adding more capacity**

From backend, validator could check the existing resources and accordingly validate the request. In future, it could check for different storage platforms (like AWS, Azure, VMWares', BareMetal etc) and further advance the validation checks.

We had cases reported where, while expanding the OCS cluster, the additional capacity is taken without further checks about feasibility of the resources. This ended up into deployment of first `n` number of additional osds and got stuck on finding no additional nodes to use. [Bug#1777073](https://bugzilla.redhat.com/show_bug.cgi?id=1777073)

Another case in which user has tampered through the CR and changed the minimum cluster initialization capacity (from 1Tib) to 100Gib. [Bug#1751331](https://bugzilla.redhat.com/show_bug.cgi?id=1751331)

## Proposal

We would like to introduce **Admission Controllers** to the OCS Project. An **Admission Controller** is a piece of code that intercepts requests to the Kubernetes API server, prior to persistence of the object, but after the request is authenticated and authorized.

## Admission Controllers

### General Overview

![AC Image](./design-pic.png)

Some more details can be found [here](https://kubernetes.io/blog/2019/03/21/a-guide-to-kubernetes-admission-controllers/).

### In OCS Operator Project

#### Goals

Our main goals are,

- Minimal changes to the existing OCS Operator project
    - there should not be any (no or very minimal) change to the existing code
- Easy to add / remove / modify validations
    - other developers should be able to modify validations

### Design

We want to deploy 'Admission Controllers' as a sidecar project, thus not creating any additional pod and thus following a minimalistic approach. The only constraints will be the usage of HTTPS port, 443, by Webhooks.

To deploy an admission controller into OCS Operator project, we have to

i. Generate TLS Certificate to authenticate the Admission Controller (AC)

ii. Configure **WebHooks** to handle the incoming validation requests to AC

Once **Admission Controller** is deployed to OCS Operator project, it will start

    1. The Webhook server 
    2. The services to this server, which are either Validating or Mutating Webhooks

The webhook requires a valid TLS cerficate configured for a connection.

WebHooks intercept object requests based on 3 parameters -

1. The operation being performed for the request ( create, update, delete )
2. The api group and version of the object (v1, v1beta)
3. The type of resource ( pod, service, storagecluster) 


#### ValidationWebhookConfiguration 

Validating admission webhooks are invoked during the validation phase of the admission process. This phase allows the enforcement of invariants on particular API resources to ensure that the resource does not change again. The Pod Node Selector is also an example of a validation admission, by ensuring that all nodeSelector fields are constrained by the node selector restrictions on the project.

Sample

```
apiVersion: admissionregistration.k8s.io/v1beta1
  kind: ValidatingWebhookConfiguration  ----> 1
  metadata:
    name: <controller_name> ----> 2
  webhooks:
  - name: <webhook_name> ----> 3
    clientConfig: ----> 4
      service:
        namespace: default  ----> 5
        name: kubernetes ----> 6
       path: <webhook_url> ----> 7
      caBundle: <cert> ----> 8
    rules: ----> 9
    - operations: ----> 10
      - <operation>
      apiGroups:
      - ""
      apiVersions:
      - "*"
      resources:
      - <resource>
    failurePolicy: <policy> ----> 11
```

1. Specifies a validating admission webhook configuration.
2. The name for the webhook admission object.
3. The name of the webhook to call.
4. Information about how to connect to, trust, and send data to the webhook server.
5. The project where the front-end service is created.
6. The name of the front-end service.
7. The webhook URL used for admission requests.
8. A PEM-encoded CA certificate that signs the server certificate used by the webhook server.
9. Rules that define when the API server should use this controller.
10. The operation that triggers the API server to call this controller are,

    - create
    - update
    - delete
    - connect

11. Specifies how the policy should proceed if the webhook admission server is unavailable. Either Ignore (allow/fail open) or Fail (block/fail closed).

On intercepting a request, the webhook will be triggered and send a `/validate` path in the url directly to our server. 
The server will be setup in the main.go code to handle any urls with `/validate`. In the function we will be receiving an AdmissionReviewRequest object, We have applied our own custom validation rules here to reject any pod that has name of reject-pod and print out an error message. In the end, we create an AdmissionReviewResponse object and set its fields to reject or admit a request to the kubernetes api

```Go
admissionReviewResponse.Response.Allowed = false/true
admissionReviewResponse.Response.Result =  &metav1.Status{
			Message: err.Error()
}
```
