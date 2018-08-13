package stub

import (
	"context"

	"github.com/wongma7/efs-provisioner-operator/pkg/apis/efs/v1alpha1"

	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/wongma7/efs-provisioner-operator/pkg/provisioner"
)

func NewHandler() sdk.Handler {
	return &Handler{}
}

type Handler struct {
	// Fill me
}

func (h *Handler) Handle(ctx context.Context, event sdk.Event) error {
	switch o := event.Object.(type) {
	case *v1alpha1.EFSProvisioner:
		return provisioner.Reconcile(o)
	}
	return nil
}