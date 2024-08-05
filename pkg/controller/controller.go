package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	routeclient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sver "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/openshift/cli-manager/api/v1alpha1"
	"github.com/openshift/cli-manager/pkg/git"
	"github.com/openshift/cli-manager/pkg/image"
	krew "github.com/openshift/cli-manager/pkg/krew/v1alpha2"
)

type DockerConfigJson struct {
	Auths DockerConfig `json:"auths"`
}

type DockerConfig map[string]DockerConfigEntry

type DockerConfigEntry struct {
	Auth string `json:"auth"`
}

type Controller struct {
	factory.Controller
	lister        cache.GenericLister
	repo          *git.Repo
	client        *kubernetes.Clientset
	dynamicClient *dynamic.DynamicClient
	route         routeclient.RouteV1Interface

	insecureHTTP bool
}

// NewCLISyncController creates CLI Sync Controller to react changes in Plugin resource
func NewCLISyncController(repo *git.Repo, informers dynamicinformer.DynamicSharedInformerFactory, client *kubernetes.Clientset, dynamicClient *dynamic.DynamicClient, route routeclient.RouteV1Interface, insecureHTTP bool, eventRecorder events.Recorder) (*Controller, error) {
	informer := informers.ForResource(schema.GroupVersionResource{
		Group:    v1alpha1.GroupVersion.Group,
		Version:  v1alpha1.GroupVersion.Version,
		Resource: "plugins",
	})

	c := &Controller{
		lister:        informer.Lister(),
		repo:          repo,
		client:        client,
		dynamicClient: dynamicClient,
		route:         route,
		insecureHTTP:  insecureHTTP,
	}

	c.Controller = factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			klog.V(4).Infof("Plugin object cought by event %v", obj)
			if obj == nil || reflect.ValueOf(obj).IsNil() {
				return ""
			}

			u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
			if err != nil {
				return ""
			}
			plugin := &v1alpha1.Plugin{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(u, plugin)
			if err != nil {
				klog.V(2).Infof("invalid object's %v key extraction is ignored", obj)
				return ""
			}
			return plugin.Name
		}, informer.Informer()).
		WithSync(c.sync).
		ToController("CLIManager", eventRecorder)
	return c, nil
}

func (c *Controller) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	pluginName := syncCtx.QueueKey()
	klog.V(4).Infof("CLI Manager sync is triggered for the key %s", pluginName)
	obj, err := c.dynamicClient.Resource(schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1alpha1",
		Resource: "plugins"}).Get(ctx, pluginName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			err = DeletePlugin(pluginName, c.repo)
			if err != nil {
				return err
			}
			klog.Infof("plugin %s is successfully deleted", pluginName)
			return nil
		} else {
			klog.Warningf("plugin %s retrieval from cache error %v", pluginName, err)
			return err
		}
	}

	if obj == nil || reflect.ValueOf(obj).IsNil() {
		klog.V(2).Infof("invalid nil object is ignored")
		return nil
	}

	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		klog.V(2).Infof("invalid object %v is ignored", obj)
		return nil
	}

	plugin := &v1alpha1.Plugin{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(u, plugin)
	if err != nil {
		klog.V(2).Infof("ignore unexpected types %+v for key %s", obj, pluginName)
		return nil
	}

	err = DeletePlugin(pluginName, c.repo)
	if err != nil {
		klog.V(2).Infof("plugin %s can not be deleted", pluginName)
	}

	err = UpsertPlugin(plugin, c.repo, c.client, c.dynamicClient, c.route, c.insecureHTTP)
	if err != nil {
		return err
	}

	return nil
}

// DeletePlugin deletes the plugin from git repository and removes
// the actuall plugin tarball from local.
func DeletePlugin(name string, repo *git.Repo) error {
	err := repo.Delete(name)
	if err != nil {
		return err
	}

	files, err := filepath.Glob(fmt.Sprintf("%s/%s_*.tar.gz", image.TarballPath, name))
	if err != nil {
		return err
	}

	for _, file := range files {
		os.Remove(file)
	}
	return nil
}

func UpsertPlugin(plugin *v1alpha1.Plugin, repo *git.Repo, client *kubernetes.Clientset, dynamicClient *dynamic.DynamicClient, route routeclient.RouteV1Interface, insecureHTTP bool) error {
	k, success, err := convertKrewPlugin(plugin, client, dynamicClient, route, insecureHTTP)
	if err != nil {
		return err
	}
	if !success {
		return nil
	}
	err = repo.Upsert(plugin.Name, k)
	if err != nil {
		return err
	}
	return nil
}

func convertKrewPlugin(plugin *v1alpha1.Plugin, client *kubernetes.Clientset, dynamicClient *dynamic.DynamicClient, route routeclient.RouteV1Interface, insecureHTTP bool) (*krew.Plugin, bool, error) {
	if plugin == nil {
		return nil, false, nil
	}
	ctx := context.Background()
	safePluginRegexp := regexp.MustCompile(`^[\w-]+$`)
	if !safePluginRegexp.MatchString(plugin.Name) {
		newCondition := metav1.Condition{
			Status:  metav1.ConditionFalse,
			Reason:  "InvalidField",
			Message: fmt.Sprintf("invalid plugin name %s", plugin.Name),
		}
		err := updateStatusCondition(ctx, plugin, dynamicClient, newCondition)
		if err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}

	if !strings.HasPrefix(plugin.Spec.Version, "v") {
		newCondition := metav1.Condition{
			Status:  metav1.ConditionFalse,
			Reason:  "InvalidField",
			Message: fmt.Sprintf("invalid version %s, should start with v like v0.0.0", plugin.Spec.Version),
		}
		err := updateStatusCondition(ctx, plugin, dynamicClient, newCondition)
		if err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	_, err := k8sver.ParseSemantic(plugin.Spec.Version)
	if err != nil {
		newCondition := metav1.Condition{
			Status:  metav1.ConditionFalse,
			Reason:  "InvalidField",
			Message: fmt.Sprintf("invalid version %s, should be in v0.0.0 format", plugin.Spec.Version),
		}
		err := updateStatusCondition(ctx, plugin, dynamicClient, newCondition)
		if err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	k := &krew.Plugin{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "krew.googlecontainertools.github.com/v1alpha2",
			Kind:       "Plugin",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: plugin.Name,
		},
		Spec: krew.PluginSpec{
			Version:          plugin.Spec.Version,
			ShortDescription: plugin.Spec.ShortDescription,
			Description:      plugin.Spec.Description,
			Caveats:          plugin.Spec.Caveats,
			Homepage:         plugin.Spec.Homepage,
		},
	}
	for _, p := range plugin.Spec.Platforms {
		fields := strings.SplitN(p.Platform, "/", 2)
		if len(fields) < 2 {
			continue
		}

		var imageAuth string
		if len(p.ImagePullSecret) > 0 {
			secrets := strings.SplitN(p.ImagePullSecret, "/", 2)
			var namespace, secret string
			if len(secrets) > 1 {
				namespace = secrets[0]
				secret = secrets[1]
			} else {
				secret = secrets[0]
			}
			// if an imagePullSecret is defined for the binary, retrieve the Secret for it
			imagePullSecret, err := client.CoreV1().Secrets(namespace).Get(ctx, secret, metav1.GetOptions{})
			if err != nil {
				newCondition := metav1.Condition{
					Status:  metav1.ConditionFalse,
					Reason:  "InvalidField",
					Message: fmt.Sprintf("error occurred %s while getting the secret %s", err, secret),
				}
				if errors.IsNotFound(err) {
					newCondition.Message = fmt.Sprintf("secret %s is not found. If secret is in another namespace, please prepend namespace as anotherns/secret_name format", secret)
				}
				err := updateStatusCondition(ctx, plugin, dynamicClient, newCondition)
				if err != nil {
					return nil, false, err
				}
				return nil, false, nil
			}

			// ensure the Secret is of the expected type
			if imagePullSecret.Type != corev1.SecretTypeDockercfg && imagePullSecret.Type != corev1.SecretTypeDockerConfigJson {
				newCondition := metav1.Condition{
					Status:  metav1.ConditionFalse,
					Reason:  "InvalidSecretType",
					Message: fmt.Sprintf("image pull secret type %s is not supported, only kubernetes.io/dockercfg and kubernetes.io/dockerconfigjson are supported", imagePullSecret.Type),
				}
				err := updateStatusCondition(ctx, plugin, dynamicClient, newCondition)
				if err != nil {
					return nil, false, err
				}
				return nil, false, nil
			}

			if imagePullSecret.Type == corev1.SecretTypeDockercfg {
				// set the .dockercfg auth information for the image puller
				imageAuth = string(imagePullSecret.Data[corev1.DockerConfigKey])
			} else if imagePullSecret.Type == corev1.SecretTypeDockerConfigJson {
				var dcr *DockerConfigJson
				err = json.Unmarshal(imagePullSecret.Data[corev1.DockerConfigJsonKey], &dcr)
				if err != nil || dcr == nil {
					newCondition := metav1.Condition{
						Status:  metav1.ConditionFalse,
						Reason:  "InvalidField",
						Message: fmt.Sprintf("unable to parse dockerjson %s to json", imagePullSecret.Name),
					}
					err := updateStatusCondition(ctx, plugin, dynamicClient, newCondition)
					if err != nil {
						return nil, false, err
					}
					return nil, false, nil
				}
				for key, val := range dcr.Auths {
					if strings.Contains(p.Image, key+"/") {
						imageAuth = val.Auth
					}
				}
			}
		}

		// attempt to pull the image down locally
		img, err := image.Pull(p.Image, imageAuth)
		if err != nil {
			newCondition := metav1.Condition{
				Status:  metav1.ConditionFalse,
				Reason:  "ImagePullError",
				Message: fmt.Sprintf("failed to pull the image error %s", err),
			}
			err := updateStatusCondition(ctx, plugin, dynamicClient, newCondition)
			if err != nil {
				return nil, false, err
			}
			return nil, false, nil
		}

		destinationFileName := fmt.Sprintf("%s/%s_%s.tar.gz", image.TarballPath, plugin.Name, strings.ReplaceAll(p.Platform, "/", "_"))
		files, err := image.Extract(img, p, destinationFileName)
		if err != nil {
			newCondition := metav1.Condition{
				Status:  metav1.ConditionFalse,
				Reason:  "ExtractFromImageError",
				Message: fmt.Sprintf("failed to extract the binary from image error %s", err),
			}
			err := updateStatusCondition(ctx, plugin, dynamicClient, newCondition)
			if err != nil {
				return nil, false, err
			}
			return nil, false, nil
		}

		if len(files) == 0 {
			newCondition := metav1.Condition{
				Status:  metav1.ConditionFalse,
				Reason:  "BinaryNotFound",
				Message: fmt.Sprintf("failed to find the binary from image, path should not be directory, symlink"),
			}
			err := updateStatusCondition(ctx, plugin, dynamicClient, newCondition)
			if err != nil {
				return nil, false, err
			}
			return nil, false, nil
		}

		dest, err := os.Open(destinationFileName)
		if err != nil {
			newCondition := metav1.Condition{
				Status:  metav1.ConditionFalse,
				Reason:  "BinaryNotFound",
				Message: fmt.Sprintf("failed to open the extracted binary %s", err),
			}
			err := updateStatusCondition(ctx, plugin, dynamicClient, newCondition)
			if err != nil {
				return nil, false, err
			}
			return nil, false, nil
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, dest); err != nil {
			dest.Close()
			newCondition := metav1.Condition{
				Status:  metav1.ConditionFalse,
				Reason:  "Sha256ChecksumError",
				Message: fmt.Sprintf("could not calculate sha256 checksum"),
			}
			err := updateStatusCondition(ctx, plugin, dynamicClient, newCondition)
			if err != nil {
				return nil, false, err
			}
			return nil, false, nil
		}

		checksum := hex.EncodeToString(hash.Sum(nil))

		r, err := route.Routes("openshift-cli-manager-operator").Get(ctx, "openshift-cli-manager", metav1.GetOptions{})
		if err != nil {
			return nil, false, fmt.Errorf("could not get the route openshift-cli-manager in openshift-cli-manager-operator namespace err: %w", err)
		}

		artifactURI := fmt.Sprintf("https://%s/cli-manager/plugins/download/?name=%s&platform=%s", r.Spec.Host, plugin.Name, strings.ReplaceAll(p.Platform, "/", "_"))
		if insecureHTTP {
			artifactURI = fmt.Sprintf("http://%s/cli-manager/plugins/download/?name=%s&platform=%s", r.Spec.Host, plugin.Name, strings.ReplaceAll(p.Platform, "/", "_"))
		}

		kp := krew.Platform{
			URI:    artifactURI,
			Sha256: checksum,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"os":   fields[0],
					"arch": fields[1],
				},
			},
			Files: []krew.FileOperation{},
			Bin:   p.Bin,
		}

		for _, f := range files {
			kp.Files = append(kp.Files, krew.FileOperation{
				From: f.From,
				To:   f.To,
			})
		}
		if len(kp.Bin) == 0 {
			kp.Bin = plugin.Name
		}
		k.Spec.Platforms = append(k.Spec.Platforms, kp)
	}

	klog.Infof("plugin %s is ready to be served", plugin.Name)
	newCondition := metav1.Condition{
		Status:  metav1.ConditionTrue,
		Reason:  "Installed",
		Message: fmt.Sprintf("plugin %s is ready to be served", plugin.Name),
	}
	err = updateStatusCondition(ctx, plugin, dynamicClient, newCondition)
	if err != nil {
		return nil, false, err
	}
	return k, true, nil
}

func updateStatusCondition(ctx context.Context, plugin *v1alpha1.Plugin, dynamic *dynamic.DynamicClient, condition metav1.Condition) error {
	condition.Type = "PluginInstalled"
	condition.LastTransitionTime = metav1.NewTime(time.Now())
	for _, conds := range plugin.Status.Conditions {
		if conds.Reason == condition.Reason && conds.Status == condition.Status && conds.Message == condition.Message {
			// No need to update again
			return nil
		}
	}
	plugin.Status.Conditions = []metav1.Condition{condition}
	unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(plugin)
	unObj := &unstructured.Unstructured{
		Object: unstructuredMap,
	}
	if err != nil {
		return fmt.Errorf("unexpected object decoding error %w", err)
	}
	_, err = dynamic.Resource(schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1alpha1",
		Resource: "plugins"}).UpdateStatus(ctx, unObj, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("plugin condition update error %w", err)
	}
	return nil
}
