package provisioner

import (
	"fmt"

	api "github.com/wongma7/efs-provisioner-operator/pkg/apis/efs/v1alpha1"

	"github.com/operator-framework/operator-sdk/pkg/sdk"
	//"github.com/sirupsen/logrus"
)

// Reconcile reconciles the vault cluster's state to the spec specified by pr
// by preparing the TLS secrets, deploying the etcd and vault cluster,
// and finally updating the vault deployment if needed.
func Reconcile(pr *api.EFSProvisioner) (err error) {
	pr = pr.DeepCopy()
	if len(pr.Spec.FSID) == 0 {
		return fmt.Errorf("FSID is required")
	}
	// TODO get and set AWS region somehow
	// Simulate initializer.
	changed := pr.SetDefaults()
	if changed {
		return sdk.Update(pr)
	}
	// After first time reconcile, phase will switch to "Running".
	if pr.Status.Phase == api.ClusterPhaseInitial {
		// TODO don't think we need this?
	}

	err = deployProvisioner(pr)
	if err != nil {
		return err
	}
	// TODO syncVaultClusterSize
	// TODO	getProvisionerStatus
	// TODO	syncUpgrade

	// TODO deployStorageClass
	err = deployStorageClass(pr)
	if err != nil {
		return err
	}

	return nil
}
