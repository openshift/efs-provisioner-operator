package efsprovisioner

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	efsv1alpha1 "github.com/openshift/efs-provisioner-operator/pkg/apis/efs/v1alpha1"
	"github.com/openshift/efs-provisioner-operator/pkg/generated"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/v1alpha1helpers"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// Add creates a new EFSProvisioner Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		log.Fatal(err)
	}
	return &ReconcileEFSProvisioner{client: mgr.GetClient(), clientset: clientset, scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("efsprovisioner-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource EFSProvisioner
	err = c.Watch(&source.Kind{Type: &efsv1alpha1.EFSProvisioner{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource Deployments and StorageClasses and requeue the owner EFSProvisioner
	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &efsv1alpha1.EFSProvisioner{},
	})
	if err != nil {
		return err
	}

	// TODO watch doesn't include owner's namespace?
	err = c.Watch(&source.Kind{Type: &storagev1.StorageClass{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &efsv1alpha1.EFSProvisioner{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileEFSProvisioner{}

// ReconcileEFSProvisioner reconciles a EFSProvisioner object
type ReconcileEFSProvisioner struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client    client.Client
	clientset *kubernetes.Clientset
	scheme    *runtime.Scheme
}

// Reconcile reads that state of the cluster for a EFSProvisioner object and makes changes based on the state read
// and what is in the EFSProvisioner.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileEFSProvisioner) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log.Printf("Reconciling EFSProvisioner %s/%s\n", request.Namespace, request.Name)

	// Fetch the EFSProvisioner instance
	instance := &efsv1alpha1.EFSProvisioner{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	pr := instance
	switch pr.Spec.ManagementState {
	case operatorv1alpha1.Unmanaged:
		return reconcile.Result{}, nil

	case operatorv1alpha1.Removed:
		// TODO cleanup, remove finalizer
		pr.Status.TaskSummary = "Remove"
		pr.Status.TargetAvailability = nil
		pr.Status.CurrentAvailability = nil
		pr.Status.Conditions = []operatorv1alpha1.OperatorCondition{
			{
				Type:   operatorv1alpha1.OperatorStatusTypeAvailable,
				Status: operatorv1alpha1.ConditionFalse,
			},
		}
		return reconcile.Result{}, r.client.Update(context.TODO(), pr)
	}
	// TODO cleanup if deletionTimestamp != nil, remove finalizer
	// TODO cleanup unwanted StorageClass (changed StorageClassName)

	// TODO move validation to CRD
	changed := false
	if pr.Spec.StorageClassName == "" {
		return reconcile.Result{}, fmt.Errorf("StorageClassName is required")
	}
	if pr.Spec.FSID == "" {
		return reconcile.Result{}, fmt.Errorf("FSID is required")
	}
	if pr.Spec.Region == "" {
		region, err := r.getRegion()
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("Region is required, failed to determine it automatically: %v", err)
		}
		pr.Spec.Region = region
		changed = true
	}

	// Simulate initializer.
	changed = pr.SetDefaults() || changed
	if changed {
		return reconcile.Result{}, r.client.Update(context.TODO(), pr)
	}

	errors := r.syncRBAC(pr)

	err = r.syncStorageClass(pr)
	if err != nil {
		errors = append(errors, fmt.Errorf("error syncing storageClass: %v", err))
	}

	previousAvailability := pr.Status.CurrentAvailability
	forceDeployment := pr.ObjectMeta.Generation != pr.Status.ObservedGeneration
	deployment, err := r.syncDeployment(pr, previousAvailability, forceDeployment)
	if err != nil {
		errors = append(errors, fmt.Errorf("error syncing deployment: %v", err))
	}

	err = r.syncStatus(pr, deployment, errors)
	if err != nil {
		errors = append(errors, fmt.Errorf("error syncing status: %v", err))
	}

	if len(errors) > 0 {
		log.Printf("errors: %v", errors)
	}
	return reconcile.Result{}, utilerrors.NewAggregate(errors)
}

const (
	provisionerName = "openshift.io/aws-efs"
	leaseName       = "openshift.io-aws-efs" // provisionerName slashes replaced with dashes
)

func (r *ReconcileEFSProvisioner) syncRBAC(pr *efsv1alpha1.EFSProvisioner) []error {
	selector := labelsForProvisioner(pr)

	errors := []error{}

	serviceAccount := resourceread.ReadServiceAccountV1OrDie(generated.MustAsset("manifests/serviceaccount.yaml"))
	serviceAccount.SetNamespace(pr.GetNamespace())
	serviceAccount.SetLabels(selector)
	if err := controllerutil.SetControllerReference(pr, serviceAccount, r.scheme); err != nil {
		errors = append(errors, err)
	}
	_, _, err := resourceapply.ApplyServiceAccount(r.clientset.CoreV1(), serviceAccount)
	if err != nil {
		errors = append(errors, fmt.Errorf("error applying serviceAccount: %v", err))
	}

	clusterRole := resourceread.ReadClusterRoleV1OrDie(generated.MustAsset("manifests/clusterrole.yaml"))
	clusterRole.SetLabels(selector)
	_, _, err = resourceapply.ApplyClusterRole(r.clientset.RbacV1(), clusterRole)
	if err != nil {
		errors = append(errors, fmt.Errorf("error applying clusterRole: %v", err))
	}

	clusterRoleBinding := resourceread.ReadClusterRoleBindingV1OrDie(generated.MustAsset("manifests/clusterrolebinding.yaml"))
	clusterRoleBinding.Subjects[0].Namespace = pr.GetNamespace()
	clusterRoleBinding.SetLabels(selector)
	_, _, err = resourceapply.ApplyClusterRoleBinding(r.clientset.RbacV1(), clusterRoleBinding)
	if err != nil {
		errors = append(errors, fmt.Errorf("error applying clusterRoleBinding: %v", err))
	}

	role := resourceread.ReadRoleV1OrDie(generated.MustAsset("manifests/role.yaml"))
	role.Namespace = pr.GetNamespace()
	role.Rules[0].ResourceNames = []string{leaseName}
	role.SetLabels(selector)
	if err := controllerutil.SetControllerReference(pr, role, r.scheme); err != nil {
		errors = append(errors, err)
	}
	_, _, err = resourceapply.ApplyRole(r.clientset.RbacV1(), role)
	if err != nil {
		errors = append(errors, fmt.Errorf("error applying role: %v", err))
	}

	roleBinding := resourceread.ReadRoleBindingV1OrDie(generated.MustAsset("manifests/rolebinding.yaml"))
	roleBinding.Namespace = pr.GetNamespace()
	roleBinding.Subjects[0].Namespace = pr.GetNamespace()
	roleBinding.SetLabels(selector)
	if err := controllerutil.SetControllerReference(pr, roleBinding, r.scheme); err != nil {
		errors = append(errors, err)
	}
	_, _, err = resourceapply.ApplyRoleBinding(r.clientset.RbacV1(), roleBinding)
	if err != nil {
		errors = append(errors, fmt.Errorf("error applying roleBinding: %v", err))
	}

	return errors
}

func (r *ReconcileEFSProvisioner) syncStorageClass(pr *efsv1alpha1.EFSProvisioner) error {
	selector := labelsForProvisioner(pr)

	// TODO use manifest yaml
	parameters := map[string]string{}
	if pr.Spec.GidAllocate != nil && *pr.Spec.GidAllocate {
		parameters["gidAllocate"] = "true"
		parameters["gidMin"] = strconv.Itoa(*pr.Spec.GidMin)
		parameters["gidMax"] = strconv.Itoa(*pr.Spec.GidMax)
	}

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: pr.Spec.StorageClassName,
		},
		Provisioner:   provisionerName,
		Parameters:    parameters,
		ReclaimPolicy: pr.Spec.ReclaimPolicy,
	}
	sc.SetLabels(selector)
	err := r.client.Create(context.TODO(), sc)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			oldSc := &storagev1.StorageClass{}
			err := r.client.Get(context.TODO(), types.NamespacedName{Name: sc.GetName(), Namespace: corev1.NamespaceAll}, oldSc)
			if err != nil {
				return err
			}
			// gidallocator handles mutation of gid range parameters
			if !equality.Semantic.DeepEqual(oldSc.Parameters, sc.Parameters) ||
				!equality.Semantic.DeepEqual(oldSc.ReclaimPolicy, sc.ReclaimPolicy) {
				err = r.client.Delete(context.TODO(), oldSc)
				if err != nil {
					return err
				}
				err = r.client.Create(context.TODO(), sc)
				if err != nil {
					return err
				}
			} else {
				err = r.client.Update(context.TODO(), sc)
				if err != nil {
					return err
				}
			}
		} else {
			return err
		}
	}
	return nil
}

func (r *ReconcileEFSProvisioner) syncDeployment(pr *efsv1alpha1.EFSProvisioner, previousAvailability *operatorv1alpha1.VersionAvailability, forceDeployment bool) (*appsv1.Deployment, error) {
	selector := labelsForProvisioner(pr)

	deployment := resourceread.ReadDeploymentV1OrDie(generated.MustAsset("manifests/deployment.yaml"))

	deployment.SetName(pr.GetName())
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
		template.Spec.SecurityContext = &corev1.PodSecurityContext{
			SupplementalGroups: []int64{*pr.Spec.SupplementalGroup},
		}
	}

	template.Spec.Containers[0].Image = pr.Spec.ImagePullSpec
	template.Spec.Containers[0].Env = []corev1.EnvVar{
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

	if err := controllerutil.SetControllerReference(pr, deployment, r.scheme); err != nil {
		return nil, err
	}
	actualDeployment, _, err := resourceapply.ApplyDeployment(r.clientset.AppsV1(), deployment, resourcemerge.ExpectedDeploymentGeneration(deployment, previousAvailability), forceDeployment)
	if err != nil {
		return nil, err
	}

	return actualDeployment, nil
}

const (
	OwnerLabelNamespace = "efs.storage.openshift.io/owner-namespace"
	OwnerLabelName      = "efs.storage.openshift.io/owner-name"
)

// labelsForProvisioner returns the labels for selecting the resources
// belonging to the given provisioner name.
func labelsForProvisioner(pr *efsv1alpha1.EFSProvisioner) map[string]string {
	return map[string]string{
		OwnerLabelNamespace: pr.Namespace,
		OwnerLabelName:      pr.Name,
	}
}

// Copied from https://github.com/openshift/service-serving-cert-signer/blob/9337a18300a63e369f34d411b2080b4bd877e7a9/pkg/operator/operator.go#L142
func (r *ReconcileEFSProvisioner) syncStatus(operatorConfig *efsv1alpha1.EFSProvisioner, deployment *appsv1.Deployment, errors []error) error {
	versionAvailability := operatorv1alpha1.VersionAvailability{}
	versionAvailability = resourcemerge.ApplyDeploymentGenerationAvailability(versionAvailability, deployment, errors...)
	operatorConfig.Status.CurrentAvailability = &versionAvailability

	// given the VersionAvailability and the status.Version, we can compute availability
	availableCondition := operatorv1alpha1.OperatorCondition{
		Type:   operatorv1alpha1.OperatorStatusTypeAvailable,
		Status: operatorv1alpha1.ConditionUnknown,
	}
	if operatorConfig.Status.CurrentAvailability != nil && operatorConfig.Status.CurrentAvailability.ReadyReplicas > 0 {
		availableCondition.Status = operatorv1alpha1.ConditionTrue
	} else {
		availableCondition.Status = operatorv1alpha1.ConditionFalse
	}
	v1alpha1helpers.SetOperatorCondition(&operatorConfig.Status.Conditions, availableCondition)

	syncSuccessfulCondition := operatorv1alpha1.OperatorCondition{
		Type:   operatorv1alpha1.OperatorStatusTypeSyncSuccessful,
		Status: operatorv1alpha1.ConditionTrue,
	}
	if operatorConfig.Status.CurrentAvailability != nil && len(operatorConfig.Status.CurrentAvailability.Errors) > 0 {
		syncSuccessfulCondition.Status = operatorv1alpha1.ConditionFalse
		syncSuccessfulCondition.Message = strings.Join(operatorConfig.Status.CurrentAvailability.Errors, "\n")
	}
	if operatorConfig.Status.TargetAvailability != nil && len(operatorConfig.Status.TargetAvailability.Errors) > 0 {
		syncSuccessfulCondition.Status = operatorv1alpha1.ConditionFalse
		if len(syncSuccessfulCondition.Message) == 0 {
			syncSuccessfulCondition.Message = strings.Join(operatorConfig.Status.TargetAvailability.Errors, "\n")
		} else {
			syncSuccessfulCondition.Message = availableCondition.Message + "\n" + strings.Join(operatorConfig.Status.TargetAvailability.Errors, "\n")
		}
	}
	v1alpha1helpers.SetOperatorCondition(&operatorConfig.Status.Conditions, syncSuccessfulCondition)
	if syncSuccessfulCondition.Status == operatorv1alpha1.ConditionTrue {
		operatorConfig.Status.ObservedGeneration = operatorConfig.ObjectMeta.Generation
	}

	// TODO status subresource? need status to increment generation and forc deployment
	return r.client.Status().Update(context.TODO(), operatorConfig)
}

const (
	NodeNameEnvVar  = "NODE_NAME"
	LabelZoneRegion = "failure-domain.beta.kubernetes.io/region"
)

func (r *ReconcileEFSProvisioner) getRegion() (string, error) {
	name, err := getNodeName()
	if err != nil {
		return "", err
	}

	node := &corev1.Node{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: corev1.NamespaceAll}, node)
	if err != nil {
		return "", err
	}

	region, ok := node.Labels[LabelZoneRegion]
	if ok {
		return region, nil
	}

	return "", fmt.Errorf("node label %s missing", LabelZoneRegion)
}

func getNodeName() (string, error) {
	nodeName, found := os.LookupEnv(NodeNameEnvVar)
	if !found {
		return "", fmt.Errorf("%s must be set", NodeNameEnvVar)
	}
	return nodeName, nil
}
