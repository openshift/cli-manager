package e2e

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/openshift/cli-manager/api/v1alpha1"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextclientv1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog/v2"

	routev1 "github.com/openshift/api/route/v1"
	routev1client "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"

	"github.com/openshift/cli-manager/test/e2e/bindata"
)

func TestMain(m *testing.M) {
	if os.Getenv("KUBECONFIG") == "" {
		klog.Errorf("KUBECONFIG environment variable not set")
		os.Exit(1)
	}

	if os.Getenv("RELEASE_IMAGE_LATEST") == "" {
		klog.Errorf("RELEASE_IMAGE_LATEST environment variable not set")
		os.Exit(1)
	}

	if os.Getenv("NAMESPACE") == "" {
		klog.Errorf("NAMESPACE environment variable not set")
		os.Exit(1)
	}

	kubeClient := getKubeClientOrDie()
	apiExtClient := getApiExtensionKubeClient()
	routeClient := getRouteClient()

	eventRecorder := events.NewKubeRecorder(kubeClient.CoreV1().Events("default"), "test-e2e", &corev1.ObjectReference{})

	ctx, cancelFnc := context.WithCancel(context.TODO())
	defer cancelFnc()

	assets := []struct {
		path           string
		readerAndApply func(objBytes []byte) error
	}{

		{
			path: "assets/00_namespace.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyNamespace(ctx, kubeClient.CoreV1(), eventRecorder, resourceread.ReadNamespaceV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/01_config.openshift.io_plugins.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyCustomResourceDefinitionV1(ctx, apiExtClient.ApiextensionsV1(), eventRecorder, resourceread.ReadCustomResourceDefinitionV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/02_clusterrole.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyClusterRole(ctx, kubeClient.RbacV1(), eventRecorder, resourceread.ReadClusterRoleV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/03_clusterrolebinding.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyClusterRoleBinding(ctx, kubeClient.RbacV1(), eventRecorder, resourceread.ReadClusterRoleBindingV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/04_deployment.yaml",
			readerAndApply: func(objBytes []byte) error {
				required := resourceread.ReadDeploymentV1OrDie(objBytes)
				// override the operator image with the one built in the CI

				// E.g. RELEASE_IMAGE_LATEST=registry.build01.ci.openshift.org/ci-op-fy99k61r/release@sha256:0d05e600baea6df9dcd453d3b72c925b8672685cd94f0615c1089af4aa39f215
				registry := strings.Split(os.Getenv("RELEASE_IMAGE_LATEST"), "/")[0]

				required.Spec.Template.Spec.Containers[0].Image = registry + "/" + os.Getenv("NAMESPACE") + "/pipeline:cli-manager"
				_, _, err := resourceapply.ApplyDeployment(
					ctx,
					kubeClient.AppsV1(),
					eventRecorder,
					required,
					1000, // any random high number
				)
				return err
			},
		},
		{
			path: "assets/05_route.yaml",
			readerAndApply: func(objBytes []byte) error {
				required := resourceread.ReadRouteV1OrDie(objBytes)
				_, _, err := applyRoute(ctx, routeClient, nil, required)
				return err
			},
		},
		{
			path: "assets/06_service.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyService(ctx, kubeClient.CoreV1(), eventRecorder, resourceread.ReadServiceV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/07_serviceaccount.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyServiceAccount(ctx, kubeClient.CoreV1(), eventRecorder, resourceread.ReadServiceAccountV1OrDie(objBytes))
				return err
			},
		},
	}

	// create required resources, e.g. namespace, crd, roles
	if err := wait.PollUntilContextTimeout(context.TODO(), 1*time.Second, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		for _, asset := range assets {
			klog.Infof("Creating %v", asset.path)
			if err := asset.readerAndApply(bindata.MustAsset(asset.path)); err != nil {
				klog.Errorf("Unable to create %v: %v", asset.path, err)
				return false, nil
			}
		}

		return true, nil
	}); err != nil {
		klog.Errorf("Unable to create CLIO resources: %v", err)
		os.Exit(1)
	}

	var cliManagerPod *corev1.Pod
	// Wait until the CLI Manager pod is running
	if err := wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 2*time.Minute, false, func(ctx context.Context) (bool, error) {
		klog.Infof("Listing pods...")
		podItems, err := kubeClient.CoreV1().Pods("openshift-cli-manager-operator").List(ctx, metav1.ListOptions{})
		if err != nil {
			klog.Errorf("Unable to list pods: %v", err)
			return false, nil
		}
		for _, pod := range podItems.Items {
			// skip if pod.Name _doesn't_ have operatorclient.OperandName (operand should have this)
			// or if it _has_ operatorclient.OperandName + '-operator'
			if !strings.HasPrefix(pod.Name, "openshift-cli-manager-") || strings.HasPrefix(pod.Name, "openshift-cli-manager-operator") {
				continue
			}
			klog.Infof("Checking pod: %v, phase: %v, deletionTS: %v\n", pod.Name, pod.Status.Phase, pod.GetDeletionTimestamp())
			if pod.Status.Phase == corev1.PodRunning && pod.GetDeletionTimestamp() == nil {
				cliManagerPod = pod.DeepCopy()
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		klog.Errorf("Unable to wait for the CLI Manager (operand) pod to run")
		os.Exit(1)
	}

	klog.Infof("CLI Manager (operand) pod running in %v", cliManagerPod.Name)

	r, err := routeClient.Routes("openshift-cli-manager-operator").Get(context.TODO(), "openshift-cli-manager", metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Unable to get route host: %v", err)
		os.Exit(1)
	}

	krewUrl := fmt.Sprintf("https://%s/cli-manager", r.Spec.Host)

	if err := wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 5*time.Minute, false, func(ctx context.Context) (bool, error) {
		klog.Infof("checking the route is alive")

		tr := &http.Transport{
			// Just a simple get request to check the route is up and running.
			// So that we can use skip tls verification.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client := &http.Client{Transport: tr}
		resp, err := client.Get(krewUrl)
		if err != nil {
			klog.Errorf("Unable to send request to %s: %v", krewUrl, err)
			return false, nil
		}
		defer resp.Body.Close()
		// we are checking notfound because basically our custom git server
		// does not serve anything and it is legit to get this error in GET request.
		// Whereas, by getting NotFound error proves that route configuration is up.
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
			return true, nil
		}
		klog.Infof("still not alive, status code %d", resp.StatusCode)
		return false, nil
	}); err != nil {
		klog.Errorf("Unable to wait for CLI Manager route")
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func TestCLIManager(t *testing.T) {
	customKrewIndexName := "test-e2e"
	routeClient := getRouteClient()

	r, err := routeClient.Routes("openshift-cli-manager-operator").Get(context.TODO(), "openshift-cli-manager", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("route get error %v", err)
	}
	krewUrl := fmt.Sprintf("https://%s/cli-manager", r.Spec.Host)

	currentPath := homedir.HomeDir() + "/.krew"
	cmd := exec.Command("oc", "krew", "index", "add", customKrewIndexName, krewUrl)
	cmd.Env = []string{
		"GIT_SSL_NO_VERIFY=true",
		"KREW_ROOT=" + currentPath,
		"KREW_OS=" + runtime.GOOS,
		"KREW_ARCH=" + runtime.GOARCH,
	}
	cmd.Env = append(cmd.Env, "PATH="+currentPath+"/bin"+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("oc krew index add operation failed %v output: %s", err, string(out))
	}

	dynamicClient := getApiDynamicClient()
	plugin := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "config.openshift.io/v1alpha1",
			"kind":       "Plugin",
			"metadata": map[string]any{
				"name": "oc",
			},
			"spec": map[string]any{
				"homepage":         "https://github.com/openshift/oc/",
				"shortDescription": "Binary for oc",
				"description":      "this is a test plugin to deliver oc",
				"version":          "v4.15.0",
				"platforms": []map[string]any{
					{
						"platform": "linux/amd64",
						"image":    "quay.io/openshift/origin-cli",
						"bin":      "oc",
						"files": []map[string]any{
							{
								"from": "/usr/bin/oc",
								"to":   ".",
							},
						},
					},
				},
			},
		},
	}

	_, err = dynamicClient.Resource(schema.GroupVersionResource{Group: "config.openshift.io", Version: "v1alpha1", Resource: "plugins"}).Create(context.TODO(), plugin, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("test plugin creation error %v", err)
	}

	err = wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		pluginName := fmt.Sprintf("%s/oc", customKrewIndexName)
		cmd := exec.Command("oc", "krew", "update")
		cmd.Env = []string{
			"GIT_SSL_NO_VERIFY=true",
			"KREW_ROOT=" + currentPath,
			"KREW_OS=" + runtime.GOOS,
			"KREW_ARCH=" + runtime.GOARCH,
		}
		cmd.Env = append(cmd.Env, "PATH="+currentPath+"/bin"+string(os.PathListSeparator)+os.Getenv("PATH"))
		err := cmd.Run()
		if err != nil {
			t.Fatalf("oc krew update operation failed %v", err)
		}

		cmd = exec.Command("oc", "krew", "search", pluginName)
		cmd.Env = []string{
			"GIT_SSL_NO_VERIFY=true",
			"KREW_ROOT=" + currentPath,
			"KREW_OS=" + runtime.GOOS,
			"KREW_ARCH=" + runtime.GOARCH,
		}
		cmd.Env = append(cmd.Env, "PATH="+currentPath+"/bin"+string(os.PathListSeparator)+os.Getenv("PATH"))
		res, err := cmd.Output()
		if err != nil {
			return false, err
		}
		if strings.Contains(string(res), pluginName) {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("plugin search failed %v", err)
	}

	cmd = exec.Command("oc", "krew", "install", fmt.Sprintf("%s/%s", customKrewIndexName, "oc"))
	cmd.Env = []string{
		"GIT_SSL_NO_VERIFY=true",
		"KREW_ROOT=" + currentPath,
		"KREW_OS=" + runtime.GOOS,
		"KREW_ARCH=" + runtime.GOARCH,
	}
	cmd.Env = append(cmd.Env, "PATH="+currentPath+"/bin"+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin installation failure %s output: %s", err, string(out))
	}

	cmd = exec.Command("oc", "oc", "version")
	cmd.Env = []string{
		"GIT_SSL_NO_VERIFY=true",
		"KREW_ROOT=" + currentPath,
		"KREW_OS=" + runtime.GOOS,
		"KREW_ARCH=" + runtime.GOARCH,
	}
	cmd.Env = append(cmd.Env, "PATH="+currentPath+"/bin"+string(os.PathListSeparator)+os.Getenv("PATH"))
	ver, err := cmd.Output()
	if err != nil {
		t.Fatalf("plugin execution failure response %s err %v", string(ver), err)
	}
	klog.Infof("plugin oc execution result \n %s", string(ver))
	if !strings.Contains(string(ver), "Client Version:") {
		t.Fatalf("unexpected output of plugin execution %s", string(ver))
	}

	unstrctrd, err := dynamicClient.Resource(schema.GroupVersionResource{Group: "config.openshift.io", Version: "v1alpha1", Resource: "plugins"}).Get(context.TODO(), "oc", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("test plugin retrieval error %v", err)
	}

	latestPlugin := &v1alpha1.Plugin{}
	err = machineryruntime.DefaultUnstructuredConverter.FromUnstructured(unstrctrd.UnstructuredContent(), latestPlugin)
	if err != nil {
		t.Fatalf("test plugin conversion error %v", err)
	}

	if len(latestPlugin.Status.Conditions) == 0 {
		t.Fatalf("unexpected empty condition of plugin oc")
	}

	if latestPlugin.Status.Conditions[0].Status != metav1.ConditionTrue || latestPlugin.Status.Conditions[0].Reason != "Installed" {
		t.Fatalf("unexpected condition of plugin %s reason %s", latestPlugin.Status.Conditions[0].Status, latestPlugin.Status.Conditions[0].Reason)
	}
}

func applyRoute(ctx context.Context, client routev1client.RoutesGetter, recorder events.Recorder, required *routev1.Route) (*routev1.Route, bool, error) {
	existing, err := client.Routes(required.Namespace).Get(ctx, required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.Routes(requiredCopy.Namespace).Create(ctx, resourcemerge.WithCleanLabelsAndAnnotations(requiredCopy).(*routev1.Route), metav1.CreateOptions{})
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	existingCopy := existing.DeepCopy()
	modified := resourcemerge.BoolPtr(false)
	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)
	specSame := equality.Semantic.DeepEqual(existingCopy.Spec, required.Spec)

	if specSame && !*modified {
		klog.V(4).Infof("%s route exists and is in the correct state", existingCopy.ObjectMeta.Name)
		return existingCopy, false, nil
	}

	existingCopy.Spec = required.Spec
	actual, err := client.Routes(required.Namespace).Update(ctx, existingCopy, metav1.UpdateOptions{})
	return actual, true, err
}

func getRouteClient() routev1client.RoutesGetter {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		klog.Errorf("Unable to build config: %v", err)
		os.Exit(1)
	}

	client, err := routev1client.NewForConfig(config)
	if err != nil {
		klog.Errorf("Unable to build client: %v", err)
		os.Exit(1)
	}
	return client
}

func getKubeClientOrDie() *k8sclient.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		klog.Errorf("Unable to build config: %v", err)
		os.Exit(1)
	}
	client, err := k8sclient.NewForConfig(config)
	if err != nil {
		klog.Errorf("Unable to build client: %v", err)
		os.Exit(1)
	}
	return client
}

func getApiDynamicClient() *dynamic.DynamicClient {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		klog.Errorf("Unable to build config: %v", err)
		os.Exit(1)
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		klog.Errorf("Unable to build client: %v", err)
		os.Exit(1)
	}
	return client
}

func getApiExtensionKubeClient() *apiextclientv1.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		klog.Errorf("Unable to build config: %v", err)
		os.Exit(1)
	}
	client, err := apiextclientv1.NewForConfig(config)
	if err != nil {
		klog.Errorf("Unable to build client: %v", err)
		os.Exit(1)
	}
	return client
}
