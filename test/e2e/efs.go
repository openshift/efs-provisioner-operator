package e2e

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	. "github.com/onsi/ginkgo"
	//. "github.com/onsi/gomega"

	//	"github.com/openshift/efs-provisioner-operator/pkg/apis/efs/v1alpha1"
	operatorsv1alpha1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/client"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	//	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	//	"k8s.io/kubernetes/pkg/api/legacyscheme"
	"k8s.io/kubernetes/test/e2e/framework"
)

const (
	olmManifestPath = "build/_output/olm/templates"
)

var _ = Describe("efs-provisioner-operator", func() {
	f := framework.NewDefaultFramework("efs-provisioner-operator")

	// filled in BeforeEach
	var c kubernetes.Interface
	var ns string

	var fsid string
	var region string
	var catalogNs string
	var outputDir string
	var templateDir string

	BeforeEach(func() {
		c = f.ClientSet
		ns = f.Namespace.Name

		fsid = os.Getenv("FSID")
		if fsid == "" {
			framework.Skipf("Skipped because env FSID is not set")
		}
		region = os.Getenv("REGION")
		if region == "" {
			framework.Logf("Region not set--using whatever is in customresource yaml")
		}

		// Namespace the catalog operator watches for catalog sources in. NOT the
		// same as WATCHED_NAMESPACES which is the namespaces it watches for
		// InstallPlans and Subscriptions in (default all namespaces).
		catalogNs = os.Getenv("CATALOG_NS")
		if catalogNs == "" {
			catalogNs = "openshift-operator-lifecycle-manager"
			framework.Logf("Catalog namespace not set--using 'openshift-operator-lifecycle-manager'")
		}

		var err error
		outputDir, err = ioutil.TempDir("", "efs-provisioner-operator")
		framework.ExpectNoError(err)
		templateDir = filepath.Join(outputDir, "olm/templates")
	})

	AfterEach(func() {
		_, _ = framework.RunKubectl("delete", "-f", outputDir)
	})

	Describe("EFS provisioner operator", func() {
		It("should deploy working EFS provisioner that creates and deletes persistent volumes", func() {
			nsFlag := fmt.Sprintf("--namespace=%v", ns)

			By("installing catalog with an efs-provisioner-operator package")
			installCatalog(c, ns, catalogNs, outputDir, templateDir)

			By("creating an efs-provisioner-operator install plan")
			framework.RunKubectlOrDie("create", "-f", mkpath("test/e2e/testing-manifests/installplan.yaml"), nsFlag)

			By("waiting for efs-provisioner-operator install plan to complete")
			framework.ExpectNoError(waitForInstallPlanComplete("efs-provisioner", ns), "waiting for efs-provisioner-operator install plan to complete")

			By("waiting for efs-provisioner-operator cluster service version to succeed")
			framework.ExpectNoError(waitForClusterServiceVersionSucceeded("efs-provisioner-operator.v0.0.1", ns))

			By("waiting for efs-provisioner-operator deployment to exist")
			framework.ExpectNoError(wait.PollImmediate(2*time.Second, 30*time.Second, func() (bool, error) {
				_, err := c.AppsV1().Deployments(ns).Get("efs-provisioner-operator", metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return false, nil
				}
				if err != nil {
					return false, err
				}
				return true, nil
			}), "waiting for efs-provisioner-operator deployment to exist")
			By("waiting for efs-provisioner-operator deployment to be ready")
			framework.ExpectNoError(framework.WaitForDeploymentWithCondition(c, ns, "efs-provisioner-operator", "MinimumReplicasAvailable", appsv1.DeploymentAvailable), "waiting for efs-provisioner-operator deployment to be ready")

			By("creating an efsprovisioner custom resource")
			createCROrDie(fsid, region, ns, outputDir)

			By("waiting for aws-efs storageclass to be created by operator")
			scName := "aws-efs"
			var sc *storagev1.StorageClass
			framework.ExpectNoError(wait.PollImmediate(2*time.Second, 30*time.Second, func() (bool, error) {
				var err error
				sc, err = c.StorageV1().StorageClasses().Get(scName, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return false, nil
				}
				if err != nil {
					return false, err
				}
				return true, nil
			}), "waiting for aws-efs storageclass to be created by operator")
			framework.Logf("%v", sc)

			By("creating a pvc")
			pvc := &v1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "pvc-",
				},
				Spec: v1.PersistentVolumeClaimSpec{
					AccessModes: []v1.PersistentVolumeAccessMode{
						v1.ReadWriteMany,
					},
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceName(v1.ResourceStorage): resource.MustParse("1Gi"),
						},
					},
					StorageClassName: &scName,
				},
			}
			var err error
			pvc, err = c.CoreV1().PersistentVolumeClaims(ns).Create(pvc)
			framework.ExpectNoError(err)

			By("waiting for pvc to have an efs pv provisioned for it")
			framework.ExpectNoError(framework.WaitForPersistentVolumeClaimPhase(v1.ClaimBound, c, ns, pvc.Name, framework.Poll, 1*time.Minute))
			defer framework.ExpectNoError(framework.DeletePersistentVolumeClaim(c, pvc.Name, ns))
		})
	})
})

func mkpath(file string) string {
	return filepath.Join(framework.TestContext.RepoRoot, file)
}

// TODO imagePullSpec?
func createCROrDie(fsid, region, ns, outputDir string) {
	nsFlag := fmt.Sprintf("--namespace=%v", ns)

	content, err := ioutil.ReadFile(mkpath("test/e2e/testing-manifests/efs_v1alpha1_efsprovisioner_cr.yaml"))
	framework.ExpectNoError(err)
	s := string(content)

	if fsid != "" {
		re := regexp.MustCompile("(\\s*)fsid:.*")
		s = re.ReplaceAllString(s, "${1}fsid: "+fsid)
	}
	if region != "" {
		re := regexp.MustCompile("(\\s*)region:.*")
		s = re.ReplaceAllString(s, "${1}region: "+region)
	}

	customResourcePath := filepath.Join(outputDir, "efs_v1alpha1_efsprovisioner_cr.yaml")
	framework.ExpectNoError(ioutil.WriteFile(customResourcePath, []byte(s), 0777))
	framework.RunKubectlOrDie("create", "-f", customResourcePath, nsFlag)
}

func installCatalog(c kubernetes.Interface, ns, catalogNs, outputDir, templateDir string) {
	templateCmd := fmt.Sprintf("helm template %s -f %s --set namespace='%s',catalog_namespace='%s',watchedNamespaces='%s' --output-dir %s", mkpath("deploy/olm/chart"), mkpath("deploy/olm/chart/values.yaml"), ns, catalogNs, ns, outputDir)
	By(fmt.Sprintf("rendering helm templates by running `%s`", templateCmd))
	cmd := exec.Command("sh", "-c", templateCmd)
	stdoutStderr, err := cmd.CombinedOutput()
	framework.ExpectNoError(err, fmt.Sprintf("%s", stdoutStderr))

	By("installing catalog from helm templates")
	createRenderedTemplateOrDie("30_09-storage-operators.catalogsource.yaml", outputDir, templateDir)
	createRenderedTemplateOrDie("30_06-storage-operators.configmap.yaml", outputDir, templateDir)
}

func createRenderedTemplateOrDie(filename, outputDir, templateDir string) {
	path := filepath.Join(outputDir, filename)
	framework.ExpectNoError(os.Rename(filepath.Join(templateDir, filename), path))
	framework.RunKubectlOrDie("create", "-f", path)
}

func waitForInstallPlanComplete(name, ns string) error {
	c, err := client.NewClient(framework.TestContext.KubeConfig)
	framework.ExpectNoError(err)
	var lastStatus operatorsv1alpha1.InstallPlanStatus
	err = wait.PollImmediate(2*time.Second, 30*time.Second, func() (bool, error) {
		installPlan, err := c.OperatorsV1alpha1().InstallPlans(ns).Get(name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		lastStatus = installPlan.Status
		if installPlan.Status.Phase == operatorsv1alpha1.InstallPlanPhaseFailed {
			return false, fmt.Errorf("InstallPlan failed with status %v", installPlan.Status)
		}
		return installPlan.Status.Phase == operatorsv1alpha1.InstallPlanPhaseComplete, nil
	})
	if err != nil {
		return fmt.Errorf("%v\nlast observed status: %v", err, lastStatus)
	}
	return nil
}

func waitForClusterServiceVersionSucceeded(name, ns string) error {
	c, err := client.NewClient(framework.TestContext.KubeConfig)
	framework.ExpectNoError(err)
	var lastStatus operatorsv1alpha1.ClusterServiceVersionStatus
	err = wait.PollImmediate(2*time.Second, 2*time.Minute, func() (bool, error) {
		csv, err := c.OperatorsV1alpha1().ClusterServiceVersions(ns).Get(name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		lastStatus = csv.Status
		if csv.Status.Phase == operatorsv1alpha1.CSVPhaseFailed {
			return false, fmt.Errorf("CSV failed with status %v", csv.Status)
		}
		return csv.Status.Phase == operatorsv1alpha1.CSVPhaseSucceeded ||
			(csv.Status.Phase == operatorsv1alpha1.CSVPhaseInstalling && csv.Status.Reason == operatorsv1alpha1.CSVReasonInstallSuccessful), nil
	})
	if err != nil {
		return fmt.Errorf("%v\nlast observed status: %v", err, lastStatus)
	}
	return nil
}

// TODO can't import self
/*
func efsProvisionerFromManifest(file string) (*v1alpha1.EFSProvisioner, error) {
	var p v1alpha1.EFSProvisioner
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}

	json, err := utilyaml.ToJSON(data)
	if err != nil {
		return nil, err
	}
	if err := runtime.DecodeInto(legacyscheme.Codecs.UniversalDecoder(), json, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
*/
