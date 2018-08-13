package provisioner

import (
	"fmt"

	api "github.com/wongma7/efs-provisioner-operator/pkg/apis/efs/v1alpha1"

	"github.com/operator-framework/operator-sdk/pkg/sdk"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	provisionerName    = "openshift.org/aws-efs"
	provisionerVolName = "pv-volume"
	provisionerVolPath = "/persistentvolumes"
	envFileSystemID    = "FILE_SYSTEM_ID"
	envAWSRegion       = "AWS_REGION"
	envProvisionerName = "PROVISIONER_NAME"
)

func deployProvisioner(p *api.EFSProvisioner) error {
	selector := labelsForProvisioner(p.GetName())

	podTempl := v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.GetName(),
			Namespace: p.GetNamespace(),
			Labels:    selector,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{provisionerContainer(p)},
			Volumes: []v1.Volume{{
				Name: provisionerVolName,
				VolumeSource: v1.VolumeSource{
					NFS: &v1.NFSVolumeSource{
						Server: p.Spec.FSID + ".efs." + p.Spec.Region + ".amazonaws.com",
						Path:   p.Spec.BasePath,
					},
				},
			}},
		},
	}
	if p.Spec.SupplementalGroup != 0 {
		podTempl.Spec.SecurityContext = &v1.PodSecurityContext{
			SupplementalGroups: []int64{p.Spec.SupplementalGroup},
		}
	}

	d := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.GetName(),
			Namespace: p.GetNamespace(),
			Labels:    selector,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &p.Spec.Replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selector},
			Template: podTempl,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
		},
	}
	addOwnerRefToObject(d, asOwner(p))
	err := sdk.Create(d)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}
func provisionerContainer(p *api.EFSProvisioner) v1.Container {
	return v1.Container{
		Name:  "provisioner",
		Image: fmt.Sprintf("%s:%s", p.Spec.BaseImage, p.Spec.Version),
		Env: []v1.EnvVar{
			{
				Name:  envFileSystemID,
				Value: p.Spec.FSID,
			},
			{
				Name:  envAWSRegion,
				Value: p.Spec.Region,
			},
			{
				Name:  envProvisionerName,
				Value: provisionerName,
			},
		},
		VolumeMounts: []v1.VolumeMount{{
			Name:      provisionerVolName,
			MountPath: provisionerVolPath,
		}},
	}
}
