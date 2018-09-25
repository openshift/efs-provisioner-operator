package v1alpha1

import (
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	defaultImagePullSpec = "openshift/origin-efs-provisioner:latest"
	defaultVersion       = "4.0.0"
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
func (p *EFSProvisioner) SetDefaults() bool {
	changed := false
	ps := &p.Spec
	if len(ps.ImagePullSpec) == 0 {
		ps.ImagePullSpec = defaultImagePullSpec
		changed = true
	}
	if len(ps.Version) == 0 {
		ps.Version = defaultVersion
		changed = true
	}
	if ps.Replicas == 0 {
		ps.Replicas = 2
		changed = true
	}
	if ps.BasePath == nil {
		basePath := "/"
		ps.BasePath = &basePath
		changed = true
	}
	if ps.GidAllocate == nil {
		gidAllocate := true
		ps.GidAllocate = &gidAllocate
		changed = true
	}
	if ps.GidMin == nil {
		gidMin := 2000
		ps.GidMin = &gidMin
		changed = true
	}
	if ps.GidMax == nil {
		gidMax := 2147483647
		ps.GidMax = &gidMax
		changed = true
	}

	return changed
}

type EFSProvisionerSpec struct {
	operatorv1alpha1.OperatorSpec `json:",inline"`

	// Number of replicas to deploy for a provisioner deployment.
	// Default: 2.
	// TODO should this be in API?
	Replicas int32 `json:"replicas"`

	// Name of storage class to create. If the storage class already exists, it will not be updated.
	//// This allows users to create their storage classes in advance.
	// Mandatory, no default.
	StorageClassName string `json:"storageClassName"`

	// ID of the EFS to use as base for dynamically provisioned PVs.
	// Such EFS must be created by admin before starting a provisioner!
	// Mandatory, no default.
	FSID string `json:"fsid"`

	// AWS region the provisioner is running in
	// TODO Region
	Region string `json:"region"`

	// Location of AWS credentials. Used to override global AWS credential from cluster config.
	// It should be empty in the usual case.
	// TODO Region (same issue)
	AWSSecrets *v1.SecretReference `json:"awsSecrets,omitempty"`

	// Subdirectory on the EFS specified by FSID that should be used as base
	// of all dynamically provisioner PVs.
	// Optional, defaults to "/"
	BasePath *string `json:"basePath,omitempty"`

	// Group that can write to the EFS. The provisioner will run with this
	// supplemental group to be able to create new PVs.
	// Optional, no default.
	SupplementalGroup *int64 `json:"supplementalGroup,omitempty"`

	// Whether to allocate a unique GID in the range gidMin-gidMax to each
	// volume. Each volume will be secured to its allocated GID. Any pod that
	// consumes the claim will be able to read/write the
	// volume because the pod will automatically receive the volume's allocated
	// GID as a supplemental group, but non-pod mounters outside the system will
	// not have read/write access unless they have the GID or root privileges.
	// Optional, defaults to true
	GidAllocate *bool `json:"gidAllocate,omitempty"`

	// Min in allocation range gidMin-gidMax when GidAllocate is true.
	// Optional, defaults to 2000.
	GidMin *int `json:"gidMin,omitempty"`

	// Max in allocation range gidMin-gidMax when GidAllocate is true.
	// Optional, defaults to 2147483647.
	GidMax *int `json:"gidMax,omitempty"`
}

type EFSProvisionerStatus struct {
	operatorv1alpha1.OperatorStatus `json:",inline"`
}
