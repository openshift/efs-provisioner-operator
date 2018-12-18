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
2. Create a CR
```
$ oc create -f ./deploy/crds/efs_v1alpha1_efsprovisioner_cr.yaml
```
