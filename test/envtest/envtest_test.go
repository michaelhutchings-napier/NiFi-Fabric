package envtest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
)

func TestEnvtestScaffold(t *testing.T) {
	assetsPath := os.Getenv("KUBEBUILDER_ASSETS")
	if assetsPath == "" {
		t.Skip("KUBEBUILDER_ASSETS is not set; install envtest assets before running this suite")
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add platform scheme: %v", err)
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
		},
	}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		if stopErr := testEnv.Stop(); stopErr != nil {
			t.Fatalf("stop envtest: %v", stopErr)
		}
	})

	k8sClient, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("create envtest client: %v", err)
	}

	cluster := &platformv1alpha1.NiFiCluster{}
	cluster.Name = "example"
	cluster.Namespace = "default"
	cluster.Spec.TargetRef.Name = "nifi"
	cluster.Spec.DesiredState = platformv1alpha1.DesiredStateRunning

	if err := k8sClient.Create(context.Background(), cluster); err != nil {
		t.Fatalf("create NiFiCluster: %v", err)
	}

	dataflow := &platformv1alpha1.NiFiDataflow{}
	dataflow.Name = "example-flow"
	dataflow.Namespace = "default"
	dataflow.Spec.ClusterRef.Name = cluster.Name
	dataflow.Spec.Source.RegistryClient.Name = "github-main"
	dataflow.Spec.Source.Bucket = "platform-flows"
	dataflow.Spec.Source.Flow = "example-flow"
	dataflow.Spec.Source.Version = "1"
	dataflow.Spec.Target.RootChildProcessGroupName = "example-flow"

	if err := k8sClient.Create(context.Background(), dataflow); err != nil {
		t.Fatalf("create NiFiDataflow: %v", err)
	}
}
