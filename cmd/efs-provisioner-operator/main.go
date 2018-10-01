package main

import (
	"context"
	"runtime"
	"time"

	"github.com/openshift/efs-provisioner-operator/pkg/stub"
	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/operator-framework/operator-sdk/pkg/util/k8sutil"
	sdkVersion "github.com/operator-framework/operator-sdk/version"

	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
)

func printVersion() {
	logrus.Infof("Go Version: %s", runtime.Version())
	logrus.Infof("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)
	logrus.Infof("operator-sdk Version: %v", sdkVersion.Version)
}

func main() {
	printVersion()

	sdk.ExposeMetricsPort()

	resource := "efs.provisioner.openshift.io/v1alpha1"
	kind := "EFSProvisioner"
	namespace, err := k8sutil.GetWatchNamespace()
	if err != nil {
		logrus.Fatalf("Failed to get watch namespace: %v", err)
	}
	resyncPeriod := 5 * time.Second
	logrus.Infof("Watching %s, %s, %s, %d", resource, kind, namespace, resyncPeriod)
	sdk.Watch(resource, kind, namespace, resyncPeriod)

	// TODO labelSelector
	resource = "storage.k8s.io/v1"
	kind = "StorageClass"
	logrus.Infof("Watching %s, %s, %s, %d", resource, kind, v1.NamespaceAll, resyncPeriod)
	sdk.Watch(resource, kind, v1.NamespaceAll, resyncPeriod, sdk.WithLabelSelector("efs-provisioner"))

	resource = "apps/v1"
	kind = "Deployment"
	logrus.Infof("Watching %s, %s, %s, %d", resource, kind, namespace, resyncPeriod)
	sdk.Watch(resource, kind, namespace, resyncPeriod, sdk.WithLabelSelector("efs-provisioner"))

	sdk.Handle(stub.NewHandler())
	sdk.Run(context.TODO())
}
