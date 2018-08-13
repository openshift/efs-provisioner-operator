package provisioner

import (
	api "github.com/wongma7/efs-provisioner-operator/pkg/apis/efs/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// addOwnerRefToObject appends the desired OwnerReference to the object
func addOwnerRefToObject(o metav1.Object, r metav1.OwnerReference) {
	o.SetOwnerReferences(append(o.GetOwnerReferences(), r))
}

// labelsForProvisioner returns the labels for selecting the resources
// belonging to the given provisioner name.
func labelsForProvisioner(name string) map[string]string {
	return map[string]string{"app": "provisioner", "provisioner": name}
}

// asOwner returns an owner reference set as the vault cluster CR
func asOwner(p *api.EFSProvisioner) metav1.OwnerReference {
	trueVar := true
	return metav1.OwnerReference{
		APIVersion: api.SchemeGroupVersion.String(),
		Kind:       api.EFSProvisionerKind,
		Name:       p.Name,
		UID:        p.UID,
		Controller: &trueVar,
	}
}
