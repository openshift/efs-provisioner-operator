package v1alpha1

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	defaultBaseImage = "openshift.com/ose-efs-provisioner"
	// version format is "<upstream-version>-<our-version>"
	defaultVersion = "1.0.0-0"
)

type ClusterPhase string

const (
	ClusterPhaseInitial ClusterPhase = ""
	ClusterPhaseRunning              = "Running"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type EFSProvisionerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []EFSProvisioner `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type EFSProvisioner struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              EFSProvisionerSpec   `json:"spec"`
	Status            EFSProvisionerStatus `json:"status,omitempty"`
}

// SetDefaults sets the default vaules for the external provisioner spec and returns true if the spec was changed
func (v *EFSProvisioner) SetDefaults() bool {
	changed := false
	vs := &v.Spec
	if vs.Replicas == 0 {
		vs.Replicas = 2
		changed = true
	}
	if len(vs.BaseImage) == 0 {
		vs.BaseImage = defaultBaseImage
		changed = true
	}
	if len(vs.Version) == 0 {
		vs.Version = defaultVersion
		changed = true
	}
	return changed
}

type EFSProvisionerSpec struct {
	// Number of replicas to deploy for a provisioner deployment.
	// Default: 2.
	// TODO don't put in API, hardcode this because we know better?
	Replicas int32 `json:"replicas,omitempty"`

	// Name of image (incl. version) with EFS provisioner. Used to override global image and version from cluster config.
	// It should be empty in the usual case.
	//// TODO: find out where the cluster config is.
	BaseImage string `json:"baseImage"`

	// Version of provisioner to be deployed.
	// TODO remove.
	Version string `json:"version"`

	// Name of storage class to create. If the storage class already exists, it will not be updated.
	//// This allows users to create their storage classes in advance.
	// Mandatory, no default.
	StorageClassName string `json:"storageClassName,omitempty"`

	// Location of AWS credentials. Used to override global AWS credential from cluster config.
	// It should be empty in the usual case.
	//// TODO: Where can we find the cluster credentials? for this, region, etc.
	AWSSecrets v1.SecretReference `json:"awsSecrets,omitempty"`

	// AWS region the provisioner is running in
	//// TODO: find out where the cluster config is.
	// TODO remove
	Region string `json:"region"`

	// ID of the EFS to use as base for dynamically provisioned PVs.
	// Such EFS must be created by admin before starting a provisioner!
	// Mandatory, no default.
	// TODO need to wait for validation block generation? https://github.com/operator-framework/operator-sdk/issues/256
	FSID string `json:"FSID"`

	// Subdirectory on the EFS specified by FSID that should be used as base
	// of all dynamically provisioner PVs.
	// Optional, defaults to "/"
	BasePath string `json:"basePath,omitempty"`

	// Whether to allocate a unique GID in the range gidMin-gidMax to each
	// volume. Each volume will be secured to its allocated GID. Any pod that
	// consumes the claim will be able to read/write the
	// volume because the pod will automatically receive the volume's allocated
	// GID as a supplemental group, but non-pod mounters outside the system will
	// not have read/write access unless they have the GID or root privileges.
	// Optional, defaults to true
	GidAllocate bool `json:"gidAllocate,omitempty"`

	// Min in allocation range gidMin-gidMax when GidAllocate is true.
	// Optional, defaults to 2000.
	GidMin int `json:"gidMin,omitempty"`

	// Max in allocation range gidMin-gidMax when GidAllocate is true.
	// Optional, defaults to 2147483647.
	GidMax int `json:"gidMax,omitempty"`

	// Group that can write to the EFS. The provisioner will run with this
	// supplemental group to be able to create new PVs.
	// Optional, no default.
	SupplementalGroup int64 `json:"supplementalGroup"`
}
type EFSProvisionerStatus struct {
	// Phase indicates the state this provisioner jumps in.
	// Phase goes as one way as below:
	//   Initial -> Running
	Phase ClusterPhase `json:"phase"`

	// Initialized indicates if the provisioner is initialized
	Initialized bool `json:"initialized"`

	// PodNames of updated provisioner replicas. Updated means the provisioner container image version
	// matches the spec's version.
	UpdatedReplicas []string `json:"updatedReplicas,omitempty"`

	// PodName of the active provisioner.
	// Only active node can serve requests.
	Active string `json:"active"`

	// PodNames of the standby provisioners.
	Standby []string `json:"standby"`
}
