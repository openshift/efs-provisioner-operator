package stub

import (
	"context"
	"fmt"
	"strconv"

	api "github.com/wongma7/efs-provisioner-operator/pkg/apis/efs/v1alpha1"
	"github.com/wongma7/efs-provisioner-operator/pkg/generated"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/operator-framework/operator-sdk/pkg/k8sclient"
	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/operator-framework/operator-sdk/pkg/util/k8sutil"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewHandler() sdk.Handler {
	return &Handler{}
}

type Handler struct{}

func (h *Handler) Handle(ctx context.Context, event sdk.Event) error {
	switch o := event.Object.(type) {
	case *api.EFSProvisioner:
		return h.sync(o)
	case *storagev1.StorageClass:
	case *appsv1.Deployment:
		pr, err := getEFSProvisioner(o)
		if err != nil {
			logrus.Errorf("error getting EFSProvisioner object: %v", err)
			return nil
		}
		return h.sync(pr)
	}
	return nil
}

func getEFSProvisioner(object metav1.Object) (*api.EFSProvisioner, error) {
	name, ok := object.GetLabels()["provisioner"]
	if !ok {
		return nil, fmt.Errorf("'provisioner' label not found on object %v", object)
	}

	namespace, err := k8sutil.GetWatchNamespace()
	if err != nil {
		return nil, err
	}

	pr := &api.EFSProvisioner{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EFSProvisioner",
			APIVersion: "efs.provisioner.openshift.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	err = sdk.Get(pr)
	if err != nil {
		return nil, err
	}

	return pr, nil
}

func (h *Handler) sync(pr *api.EFSProvisioner) error {
	pr = pr.DeepCopy()
	if pr.Spec.StorageClassName == "" {
		return fmt.Errorf("StorageClassName is required")
	}
	if pr.Spec.Region == "" {
		// TODO Region
	}
	if pr.Spec.FSID == "" {
		return fmt.Errorf("FSID is required")
	}

	// Simulate initializer.
	changed := pr.SetDefaults()
	if changed {
		return sdk.Update(pr)
	}

	err := h.syncDeployment(pr)
	if err != nil {
		return fmt.Errorf("error syncing deployment: %v", err)
	}

	err = h.syncStorageClass(pr)
	if err != nil {
		return fmt.Errorf("error syncing storageClass: %v", err)
	}

	err = h.syncRBAC(pr)
	if err != nil {
		return fmt.Errorf("error syncing RBAC: %v", err)
	}
	// prs, err := getProvisionerStatus(pr)

	// return updateProvisionerStatus(pr)
	return nil
}

const (
	provisionerName    = "openshift.org/aws-efs"
	provisionerVolName = "pv-volume"
	leaseName          = "openshift.org-aws-efs" // provisionerName slashes replaced with dashes
)

func (h *Handler) syncDeployment(pr *api.EFSProvisioner) error {
	selector := labelsForProvisioner(pr.GetName())

	// TODO yaml
	podTempl := v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Name: pr.GetName(),
			// TODO namespace
			Namespace: pr.GetNamespace(),
			Labels:    selector,
		},
		Spec: v1.PodSpec{
			Volumes: []v1.Volume{{
				Name: provisionerVolName,
				VolumeSource: v1.VolumeSource{
					NFS: &v1.NFSVolumeSource{
						// TODO Region
						Server: pr.Spec.FSID + ".efs." + pr.Spec.Region + ".amazonaws.com",
						Path:   *pr.Spec.BasePath,
					},
				},
			}},
			Containers:         []v1.Container{provisionerContainer(pr)},
			ServiceAccountName: "efs-provisioner",
		},
	}
	if pr.Spec.SupplementalGroup != nil {
		podTempl.Spec.SecurityContext = &v1.PodSecurityContext{
			SupplementalGroups: []int64{*pr.Spec.SupplementalGroup},
		}
	}

	d := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: pr.Spec.StorageClassName,
			// TODO namespace
			Namespace: pr.GetNamespace(),
			Labels:    selector,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &pr.Spec.Replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selector},
			Template: podTempl,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
		},
	}
	addOwnerRefToObject(d, asOwner(pr))

	forceDeployment := pr.ObjectMeta.Generation != pr.Status.ObservedGeneration

	_, _, err := resourceapply.ApplyDeployment(k8sclient.GetKubeClient().AppsV1(), d, 0, forceDeployment)

	return err
}

func (h *Handler) syncStorageClass(pr *api.EFSProvisioner) error {
	selector := labelsForProvisioner(pr.GetName())

	// TODO use manifest yaml
	parameters := map[string]string{}
	if pr.Spec.GidAllocate != nil && *pr.Spec.GidAllocate {
		parameters["gidAllocate"] = "true"
		parameters["gidMin"] = strconv.Itoa(*pr.Spec.GidMin)
		parameters["gidMax"] = strconv.Itoa(*pr.Spec.GidMax)
	}

	sc := &storagev1.StorageClass{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StorageClass",
			APIVersion: "storage.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   pr.Spec.StorageClassName,
			Labels: selector,
		},
		Provisioner: provisionerName,
		Parameters:  parameters,
	}
	// https://github.com/openshift/library-go/blob/master/pkg/controller/ownerref.go
	// TODO OwnerRef
	addOwnerRefToObject(sc, asOwner(pr))
	err := sdk.Create(sc)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			// TODO ...
			err = sdk.Update(sc)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func (h *Handler) syncRBAC(pr *api.EFSProvisioner) error {
	serviceAccount := resourceread.ReadServiceAccountV1OrDie(generated.MustAsset("assets/provisioner/serviceaccount.yaml"))
	addOwnerRefToObject(serviceAccount, asOwner(pr))

	clusterRole := resourceread.ReadClusterRoleV1OrDie(generated.MustAsset("assets/provisioner/clusterrole.yaml"))
	// TODO OwnerRef
	addOwnerRefToObject(clusterRole, asOwner(pr))
	_, _, err := resourceapply.ApplyClusterRole(k8sclient.GetKubeClient().RbacV1(), clusterRole)
	if err != nil {
		return fmt.Errorf("error applying clusterRole: %v", err)
	}

	clusterRoleBinding := resourceread.ReadClusterRoleBindingV1OrDie(generated.MustAsset("assets/provisioner/clusterrolebinding.yaml"))
	// TODO OwnerRef
	addOwnerRefToObject(clusterRoleBinding, asOwner(pr))
	_, _, err = resourceapply.ApplyClusterRoleBinding(k8sclient.GetKubeClient().RbacV1(), clusterRoleBinding)
	if err != nil {
		return fmt.Errorf("error applying clusterRoleBinding: %v", err)
	}

	role := resourceread.ReadRoleV1OrDie(generated.MustAsset("assets/provisioner/role.yaml"))
	// TODO namespace
	role.Namespace = pr.GetNamespace()
	role.Rules[0].ResourceNames = []string{leaseName}
	addOwnerRefToObject(role, asOwner(pr))
	_, _, err = resourceapply.ApplyRole(k8sclient.GetKubeClient().RbacV1(), role)
	if err != nil {
		return fmt.Errorf("error applying role: %v", err)
	}

	roleBinding := resourceread.ReadRoleBindingV1OrDie(generated.MustAsset("assets/provisioner/rolebinding.yaml"))
	// TODO namespace
	roleBinding.Namespace = pr.GetNamespace()
	roleBinding.Subjects[0].Namespace = pr.GetNamespace()
	addOwnerRefToObject(roleBinding, asOwner(pr))
	_, _, err = resourceapply.ApplyRoleBinding(k8sclient.GetKubeClient().RbacV1(), roleBinding)
	if err != nil {
		return fmt.Errorf("error applying roleBinding: %v", err)
	}

	return nil
}

func provisionerContainer(p *api.EFSProvisioner) v1.Container {
	return v1.Container{
		Name:  "provisioner",
		Image: p.Spec.ImagePullSpec,
		Env: []v1.EnvVar{
			{
				Name:  "FILE_SYSTEM_ID",
				Value: p.Spec.FSID,
			},
			{
				Name: "AWS_REGION",
				// TODO Region
				Value: p.Spec.Region,
			},
			{
				Name:  "PROVISIONER_NAME",
				Value: provisionerName,
			},
		},
		VolumeMounts: []v1.VolumeMount{{
			Name:      provisionerVolName,
			MountPath: "/persistentvolumes",
		}},
	}
}

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
