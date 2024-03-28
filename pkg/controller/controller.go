package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	routeclient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

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
	lister cache.GenericLister
	repo   *git.Repo
	client *kubernetes.Clientset
	route  routeclient.RouteV1Interface
}

// NewCLISyncController creates CLI Sync Controller to react changes in Plugin resource
func NewCLISyncController(repo *git.Repo, informers dynamicinformer.DynamicSharedInformerFactory, client *kubernetes.Clientset, route routeclient.RouteV1Interface, eventRecorder events.Recorder) (*Controller, error) {
	informer := informers.ForResource(schema.GroupVersionResource{
		Group:    v1alpha1.GroupVersion.Group,
		Version:  v1alpha1.GroupVersion.Version,
		Resource: "plugins",
	})

	c := &Controller{
		lister: informer.Lister(),
		repo:   repo,
		client: client,
		route:  route,
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
	obj, err := c.lister.Get(pluginName)
	if err != nil {
		if errors.IsNotFound(err) {
			err = DeletePlugin(pluginName, c.repo)
			if err != nil {
				return err
			}
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

	err = UpsertPlugin(plugin, c.repo, c.client, c.route)
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

	os.Remove(fmt.Sprintf("%s/%s_*.tar.gz", image.TarballPath, name))
	return nil
}

func UpsertPlugin(plugin *v1alpha1.Plugin, repo *git.Repo, client *kubernetes.Clientset, route routeclient.RouteV1Interface) error {
	k, err := convertKrewPlugin(plugin, client, route)
	if err != nil {
		return err
	}
	err = repo.Upsert(plugin.Name, k)
	if err != nil {
		return err
	}
	return nil
}

func convertKrewPlugin(plugin *v1alpha1.Plugin, client *kubernetes.Clientset, route routeclient.RouteV1Interface) (*krew.Plugin, error) {
	if plugin == nil {
		return nil, nil
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
			imagePullSecret, err := client.CoreV1().Secrets(namespace).Get(context.Background(), secret, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("misconfigured Plugin: name: %s, platform: %s, error while getting imagePullSecret %s: %v", plugin.Name, p, p.ImagePullSecret, err)
			}

			// ensure the Secret is of the expected type
			if imagePullSecret.Type != corev1.SecretTypeDockercfg && imagePullSecret.Type != corev1.SecretTypeDockerConfigJson {
				return nil, fmt.Errorf("misconfigured Plugin: name: %s, platform: %s, error: configured imagePullSecret %s for given platform combination is not of type: %s or %s", plugin.Name, p, p.ImagePullSecret, corev1.SecretTypeDockercfg, corev1.SecretTypeDockerConfigJson)
			}

			if imagePullSecret.Type == corev1.SecretTypeDockercfg {
				// set the .dockercfg auth information for the image puller
				imageAuth = string(imagePullSecret.Data[corev1.DockerConfigKey])
			} else if imagePullSecret.Type == corev1.SecretTypeDockerConfigJson {
				var dcr *DockerConfigJson
				err = json.Unmarshal(imagePullSecret.Data[corev1.DockerConfigJsonKey], &dcr)
				if err != nil || dcr == nil {
					return nil, fmt.Errorf("unable to parse dockerjson %s to json", imagePullSecret.Name)
				}
				for _, val := range dcr.Auths {
					imageAuth = val.Auth
				}
			}
		}

		// attempt to pull the image down locally
		img, err := image.Pull(p.Image, imageAuth)
		if err != nil {
			return nil, fmt.Errorf("could not pull image: name: %s, error: %v for Plugin: name: %s, platform: %s", p.Image, err, plugin.Name, p)
		}

		os.RemoveAll(fmt.Sprintf("%s/%s_*.tar.gz", image.TarballPath, plugin.Name))
		destinationFileName := fmt.Sprintf("%s/%s_%s.tar.gz", image.TarballPath, plugin.Name, strings.ReplaceAll(p.Platform, "/", "_"))
		files, err := image.Extract(img, p, destinationFileName)
		if err != nil {
			return nil, err
		}

		dest, err := os.Open(destinationFileName)
		if err != nil {
			return nil, err
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, dest); err != nil {
			dest.Close()
			return nil, fmt.Errorf("could not calculate sha256 checksum: name: %s, error: %v for Plugin: name: %s, platform: %s", p.Image, err, plugin.Name, p)
		}

		checksum := hex.EncodeToString(hash.Sum(nil))

		r, err := route.Routes("openshift-cli-manager-operator").Get(context.Background(), "openshift-cli-manager", metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("could not get the route cli-manager in openshift-cli-manager namespace err: %w", err)
		}

		klog.Infof("plugin %s is ready to be served", plugin.Name)
		kp := krew.Platform{
			URI:    fmt.Sprintf("https://%s/cli-manager/plugins/download/?name=%s&platform=%s", r.Spec.Host, plugin.Name, strings.ReplaceAll(p.Platform, "/", "_")),
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
		k.Spec.Platforms = append(k.Spec.Platforms, kp)
	}

	return k, nil
}
