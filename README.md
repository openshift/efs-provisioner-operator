# efs-provisioner-operator
Operator for the EFS provisioner

## Usage

### OLM
> TODO

### YAML
1. Create CRD, namespace, RBAC rules, service account, & operator
```
$ oc apply -f ./deploy/
```
2. Edit the CR and create it
```
$ oc create -f ./deploy/crds/efs_v1alpha1_efsprovisioner_cr.yaml -n=openshift-efs-provisioner-operator
```
3. Describe the CR to get the status as the operator reconciles the CR
4. Edit the CR to make configuration changes as needed

## Conventional (non-operator) deployment

The operator automatically creates and manages the assets in assets/. It is possible also to create and manage them yourself.

See also: https://github.com/kubernetes-incubator/external-storage/tree/master/aws/efs

1. Create storage class and cluster RBAC rules
```
$ oc create -f assets/class.yaml -f assets/clusterrole.yaml -f assets/clusterrolebinding.yaml
```
2. Create a namespace
```
$ oc create ns efs-provisioner
```
3. Create namespace RBAC rules
```
$ oc create -f assets/role.yaml -f assets/rolebinding.yaml -f assets/serviceaccount.yaml -n efs-provisioner
```
4. Edit the deployment and create it
```
$ oc create -f assets/deployment.yaml -n
```
5. Describe the deployment to get its status
6. Edit the deployment or edit/recreate the storage class to make configuration changes as needed
