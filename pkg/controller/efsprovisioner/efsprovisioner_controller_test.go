package efsprovisioner

import (
	"testing"

	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	"github.com/openshift/efs-provisioner-operator/pkg/apis"
	"github.com/openshift/efs-provisioner-operator/pkg/apis/efs/v1alpha1"
	"github.com/openshift/efs-provisioner-operator/pkg/controller/efsprovisioner/fake"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	cgfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	cgtesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	defaultName      = "test"
	defaultNamespace = "openshift-efs-provisioner-operator"
	defaultLabels    = map[string]string{
		OwnerLabelName:      defaultName,
		OwnerLabelNamespace: defaultNamespace,
	}

	uid                types.UID = "test"
	blockOwnerDeletion           = true
	isController                 = true
	defaultOwner                 = metav1.OwnerReference{
		APIVersion:         "efs.storage.openshift.io/v1alpha1",
		Kind:               "EFSProvisioner",
		Name:               defaultName,
		UID:                uid,
		BlockOwnerDeletion: &blockOwnerDeletion,
		Controller:         &isController,
	}

	deleteReclaimPolicy = corev1.PersistentVolumeReclaimDelete
	storageClass        = &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "aws-efs",
			Labels: defaultLabels,
		},
		Provisioner:   "openshift.io/aws-efs",
		Parameters:    map[string]string{"gidAllocate": "true", "gidMax": "2147483647", "gidMin": "2000"},
		ReclaimPolicy: &deleteReclaimPolicy,
	}

	replicas   int32 = 1
	deployment       = &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            defaultName,
			Namespace:       defaultNamespace,
			Labels:          defaultLabels,
			OwnerReferences: []metav1.OwnerReference{defaultOwner},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: defaultLabels,
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: defaultLabels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "efs-provisioner",
					Containers: []corev1.Container{
						{
							Name:  "efs-provisioner",
							Image: "quay.io/external_storage/efs-provisioner",
							Env: []corev1.EnvVar{
								{
									Name:  "FILE_SYSTEM_ID",
									Value: "fs-asdf",
								},
								{
									Name:  "AWS_REGION",
									Value: "us-east-2",
								},
								{
									Name:  "PROVISIONER_NAME",
									Value: "openshift.io/aws-efs",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "pv-volume",
									MountPath: "/persistentvolumes",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "pv-volume",
							VolumeSource: corev1.VolumeSource{
								NFS: &corev1.NFSVolumeSource{
									Path:   "/",
									Server: "fs-asdf.efs.us-east-2.amazonaws.com",
								},
							},
						},
					},
				},
			},
		},
	}
)

func TestReconciler(t *testing.T) {
	tests := []struct {
		name string
		cr   *v1alpha1.EFSProvisioner
	}{
		{
			name: "simple",
			cr: &v1alpha1.EFSProvisioner{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultName,
					Namespace: defaultNamespace,
					UID:       uid,
				},
				Spec: v1alpha1.EFSProvisionerSpec{
					OperatorSpec: operatorv1alpha1.OperatorSpec{
						ImagePullSpec: "quay.io/external_storage/efs-provisioner",
					},
					StorageClassName: "aws-efs",
					FSID:             "fs-asdf",
					Region:           "us-east-2",
				},
			},
		},
	}

	for _, test := range tests {
		// Create an ObjectTracker...because we use both controller-runtime & client-go client (oops), create a fake of each with the same tracker
		clientScheme := scheme.Scheme
		apis.AddToScheme(clientScheme)

		initObjs := []runtime.Object{test.cr}
		tracker := cgtesting.NewObjectTracker(clientScheme, scheme.Codecs.UniversalDecoder())
		for _, obj := range initObjs {
			err := tracker.Add(obj)
			if err != nil {
				t.Fatalf("failed to add object %v: %v", obj, err)
			}
		}

		// https://github.com/kubernetes-sigs/controller-runtime/blob/5558165425ef09b6e5c3f61d3457fbeca0d6d579/pkg/client/fake/client.go
		client := fake.NewFakeClientWithSchemeAndTracker(clientScheme, tracker)

		// https://github.com/kubernetes/client-go/blob/d380bc8c2d7c70a805ae9fb5fa5a780eb5cff630/kubernetes/fake/clientset_generated.go
		clientset := &cgfake.Clientset{}
		clientset.AddReactor("*", "*", cgtesting.ObjectReaction(tracker))
		clientset.AddWatchReactor("*", func(action cgtesting.Action) (handled bool, ret watch.Interface, err error) {
			gvr := action.GetResource()
			ns := action.GetNamespace()
			watch, err := tracker.Watch(gvr, ns)
			if err != nil {
				return false, nil, err
			}
			return true, watch, nil
		})

		reconciler := ReconcileEFSProvisioner{
			client:    client,
			clientset: clientset,
			scheme:    clientScheme,
		}

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: test.cr.Namespace,
				Name:      test.cr.Name,
			},
		}

		_, err := reconciler.Reconcile(req)
		if err != nil {
			t.Error(err)
		}

		sc, _ := clientset.StorageV1().StorageClasses().Get("aws-efs", metav1.GetOptions{})
		if !apiequality.Semantic.DeepEqual(storageClass, sc) {
			t.Errorf("expected \n %+v got \n %+v", storageClass, sc)
		}

		d, _ := clientset.AppsV1().Deployments(defaultNamespace).Get(defaultName, metav1.GetOptions{})
		// TODO why DeepEqual returns false?
		if !apiequality.Semantic.DeepDerivative(deployment, d) {
			t.Errorf("expected \n %+v got \n %+v", deployment, d)
		}
	}
}
