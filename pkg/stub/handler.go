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
		if event.Deleted {
			// TODO Deleted
		}
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

func (h *Handler) sync(pr *api.EFSProvisioner) error {
	pr = pr.DeepCopy()
	if pr.Spec.StorageClassName == "" {
		return fmt.Errorf("StorageClassName is required")
	}
	if pr.Spec.FSID == "" {
		return fmt.Errorf("FSID is required")
	}
	if pr.Spec.Region == "" {
		// TODO Region
	}

	// Simulate initializer.
	changed := pr.SetDefaults()
	if changed {
		return sdk.Update(pr)
	}

	// TODO append to errors
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

	// TODO Status
	// prs, err := getProvisionerStatus(pr)

	// return updateProvisionerStatus(pr)
	return nil
}

const (
	provisionerName    = "openshift.io/aws-efs"
	provisionerVolName = "pv-volume"
	leaseName          = "openshift.io-aws-efs" // provisionerName slashes replaced with dashes
)

func (h *Handler) syncDeployment(pr *api.EFSProvisioner) error {
	selector := labelsForProvisioner(pr.GetName())

	deployment := resourceread.ReadDeploymentV1OrDie(generated.MustAsset("manifests/deployment.yaml"))

	deployment.SetName(pr.GetName())
	// TODO namespace
	deployment.SetNamespace(pr.GetNamespace())
	deployment.SetLabels(selector)

	deployment.Spec.Replicas = &pr.Spec.Replicas
	deployment.Spec.Selector = &metav1.LabelSelector{MatchLabels: selector}

	template := &deployment.Spec.Template

	template.SetLabels(selector)

	server := pr.Spec.FSID + ".efs." + pr.Spec.Region + ".amazonaws.com"
	template.Spec.Volumes[0].VolumeSource.NFS.Server = server
	template.Spec.Volumes[0].VolumeSource.NFS.Path = *pr.Spec.BasePath

	if pr.Spec.SupplementalGroup != nil {
		template.Spec.SecurityContext = &v1.PodSecurityContext{
			SupplementalGroups: []int64{*pr.Spec.SupplementalGroup},
		}
	}

	template.Spec.Containers[0].Image = pr.Spec.ImagePullSpec
	template.Spec.Containers[0].Env = []v1.EnvVar{
		{
			Name:  "FILE_SYSTEM_ID",
			Value: pr.Spec.FSID,
		},
		{
			Name: "AWS_REGION",
			// TODO Region
			Value: pr.Spec.Region,
		},
		{
			Name:  "PROVISIONER_NAME",
			Value: provisionerName,
		},
	}

	addOwnerRefToObject(deployment, asOwner(pr))

	// TODO
	forceDeployment := pr.ObjectMeta.Generation != pr.Status.ObservedGeneration
	_, _, err := resourceapply.ApplyDeployment(k8sclient.GetKubeClient().AppsV1(), deployment, -1, forceDeployment)

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
	// TODO OwnerRef
	// https://github.com/openshift/library-go/blob/master/pkg/controller/ownerref.go
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
	// TODO append to errors

	serviceAccount := resourceread.ReadServiceAccountV1OrDie(generated.MustAsset("manifests/serviceaccount.yaml"))
	// TODO namespace
	serviceAccount.Namespace = pr.GetNamespace()
	addOwnerRefToObject(serviceAccount, asOwner(pr))
	_, _, err := resourceapply.ApplyServiceAccount(k8sclient.GetKubeClient().CoreV1(), serviceAccount)
	if err != nil {
		return fmt.Errorf("error applying serviceAccount: %v", err)
	}

	clusterRole := resourceread.ReadClusterRoleV1OrDie(generated.MustAsset("manifests/clusterrole.yaml"))
	// TODO OwnerRef
	addOwnerRefToObject(clusterRole, asOwner(pr))
	_, _, err = resourceapply.ApplyClusterRole(k8sclient.GetKubeClient().RbacV1(), clusterRole)
	if err != nil {
		return fmt.Errorf("error applying clusterRole: %v", err)
	}

	clusterRoleBinding := resourceread.ReadClusterRoleBindingV1OrDie(generated.MustAsset("manifests/clusterrolebinding.yaml"))
	// TODO namespace
	clusterRoleBinding.Subjects[0].Namespace = pr.GetNamespace()
	// TODO OwnerRef
	addOwnerRefToObject(clusterRoleBinding, asOwner(pr))
	_, _, err = resourceapply.ApplyClusterRoleBinding(k8sclient.GetKubeClient().RbacV1(), clusterRoleBinding)
	if err != nil {
		return fmt.Errorf("error applying clusterRoleBinding: %v", err)
	}

	role := resourceread.ReadRoleV1OrDie(generated.MustAsset("manifests/role.yaml"))
	// TODO namespace
	role.Namespace = pr.GetNamespace()
	role.Rules[0].ResourceNames = []string{leaseName}
	addOwnerRefToObject(role, asOwner(pr))
	_, _, err = resourceapply.ApplyRole(k8sclient.GetKubeClient().RbacV1(), role)
	if err != nil {
		return fmt.Errorf("error applying role: %v", err)
	}

	roleBinding := resourceread.ReadRoleBindingV1OrDie(generated.MustAsset("manifests/rolebinding.yaml"))
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

// addOwnerRefToObject appends the desired OwnerReference to the object
func addOwnerRefToObject(o metav1.Object, r metav1.OwnerReference) {
	o.SetOwnerReferences(append(o.GetOwnerReferences(), r))
}

// labelsForProvisioner returns the labels for selecting the resources
// belonging to the given provisioner name.
func labelsForProvisioner(name string) map[string]string {
	return map[string]string{"app": "efs-provisioner", "efs-provisioner": name}
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

func getEFSProvisioner(object metav1.Object) (*api.EFSProvisioner, error) {
	name, ok := object.GetLabels()["efs-provisioner"]
	if !ok {
		return nil, fmt.Errorf("'efs-provisioner' label not found on object %v", object)
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
