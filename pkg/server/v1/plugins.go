package v1

import (
	"bytes"
	"context"
	"fmt"
	configv1 "github.com/openshift/cli-manager/api/v1"
	"github.com/openshift/cli-manager/pkg/image"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetBinariesFromImage gets the binaries from the named plugin's platform.
// The returned `io.Reader` is a tar.gz archive of the binaries.
func getBinariesFromImage(cli client.Client, name, platform string) (io.Reader, error) {
	plugin := &configv1.Plugin{}
	// TODO: use ocp based client
	/*if err := cli.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, plugin); err != nil {
		return nil, err
	}*/

	// make sure Plugin has platforms
	if len(plugin.Spec.Platforms) == 0 {
		return nil, fmt.Errorf("misconfigured Plugin: name: %s, error: there are no platforms specified for the given Plugin", name)
	}

	// get binaries list for given platform
	var pluginPlatform *configv1.PluginPlatform
	for _, plat := range plugin.Spec.Platforms {
		if plat.Platform == platform {
			pluginPlatform = &plat
			break
		}
	}

	// return an error if there is no binary for the given operating system and architecture combination
	if pluginPlatform == nil {
		// we return this type of error instead of using `fmt.Errorf` so that `errors.IsNotFound(err)` will return true, and allow an HTTP handler to return a 404 status code
		return nil, &errors.StatusError{
			ErrStatus: metav1.Status{
				Status:  metav1.StatusFailure,
				Code:    http.StatusNotFound,
				Reason:  metav1.StatusReasonNotFound,
				Details: &metav1.StatusDetails{},
				Message: fmt.Sprintf("desired Plugin does not have a binary for the requested platform combination: name: %s, platform: %s", name, platform),
			},
		}
	}

	// start configuring the image puller
	pullOptions := &image.PullOptions{}
	if len(pluginPlatform.ImagePullSecret) > 0 {
		// if an imagePullSecret is defined for the binary, retrieve the Secret for it
		imagePullSecret := &corev1.Secret{}
		// TODO: use ocp based client
		/*if err := cli.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: pluginPlatform.ImagePullSecret}, imagePullSecret); err != nil {
			return nil, fmt.Errorf("misconfigured Plugin: name: %s, platform: %s, error while getting imagePullSecret %s: %v", name, platform, pluginPlatform.ImagePullSecret, err)
		}*/

		// ensure the Secret is of the expected type
		if imagePullSecret.Type != corev1.SecretTypeDockercfg {
			return nil, fmt.Errorf("misconfigured Plugin: name: %s, platform: %s, error: configured imagePullSecret %s for given platform combination is not of type: %s", name, platform, pluginPlatform.ImagePullSecret, corev1.SecretTypeDockercfg)
		}

		// set the .dockercfg auth information for the image puller
		pullOptions.Auth = string(imagePullSecret.Data[corev1.DockerConfigKey])
	}

	// attempt to pull the image down locally
	img, err := image.Pull(pluginPlatform.Image, pullOptions)
	if err != nil {
		return nil, fmt.Errorf("could not pull image: name: %s, error: %v for Plugin: name: %s, platform: %s", pluginPlatform.Image, err, name, platform)
	}

	// create extract options and the buffer to hold tar.gz archive
	extractOptions := &image.ExtractOptions{
		Targets:     []string{},
		Destination: &bytes.Buffer{},
	}

	// loop through binaries and configure the extractor
	for _, file := range pluginPlatform.Files {
		extractOptions.Targets = append(extractOptions.Targets, file.From)
	}

	// attempt to extract and write the raw binary to the body of the response
	if err := image.Extract(img, extractOptions); err != nil {
		return nil, fmt.Errorf("unable to extract binary from image: name: %s, platform: %s, image: %s, error: %v", name, pluginPlatform.Platform, pluginPlatform.Image, err)
	}

	return extractOptions.Destination.(*bytes.Buffer), nil
}

func HandleDownloadPlugin(w http.ResponseWriter, r *http.Request, cli client.Client) {
	// validate user input
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get("name")
	if len(name) == 0 {
		http.Error(w, "missing name in query", http.StatusBadRequest)
		return
	}

	platform := r.URL.Query().Get("platform")
	if len(platform) == 0 {
		http.Error(w, "missing platform in query", http.StatusBadRequest)
		return
	}

	filename := name + ".tar.gz"

	// set the appropriate response headers for downloading a binary
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Content-Transfer-Encoding", "binary")

	// get the requested Plugin resources
	reader, err := getBinariesFromImage(cli, name, platform)
	if err != nil {
		if err != nil {
			if errors.IsNotFound(err) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}

			http.Error(w, fmt.Sprintf("getting Plugin: name: %s, platform: %s err: %w", name, platform, err), http.StatusInternalServerError)
			return
		}
	}

	_, err = io.Copy(w, reader)
	if err != nil {
		http.Error(w, fmt.Sprintf("getting Plugin: name: %s, platform: %s err: %w", name, platform, err), http.StatusInternalServerError)
		return
	}
}

// HandleGitAdversitement handles the git advertisement requests done by client tools
// relying on git compatibility. This function only supports upload-pack requests to limit
// the supported functionality only to git fetch and git clone.
func HandleGitAdversitement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	vals := r.URL.Query()
	if len(vals) != 1 {
		http.Error(w, "too many query parameters", http.StatusBadRequest)

		return
	}

	name := vals.Get("service")
	if name != transport.UploadPackServiceName {
		http.Error(w, "invalid service name", http.StatusForbidden)

		return
	}

	// We are using native git command execution instead of go-git library.
	// Because go-git does not properly work on some git requests (especially git fetch).
	// Besides, relying on git tool for such a simple but crucial functionality for our case
	// would be better for long term.
	cmd := exec.CommandContext(context.TODO(), "git", "upload-pack", "--stateless-rpc", "--advertise-refs", KrewGitRepositoryPath)
	errbuf, outbuf := &bytes.Buffer{}, &bytes.Buffer{}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = r.Body, outbuf, io.MultiWriter(errbuf, os.Stderr)
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("endpoint failure: %s", err), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
	w.Header().Add("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(
		func(str string) []byte {
			s := strconv.FormatInt(int64(len(str)+4), 16)
			if len(s)%4 != 0 {
				s = strings.Repeat("0", 4-len(s)%4) + s
			}
			return []byte(s + str)
		}("# service=git-upload-pack"))
	w.Write([]byte("0000"))
	w.Write(outbuf.Bytes())
}

func HandleGitUploadPack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// We are using native git command execution instead of go-git library.
	// Because go-git does not properly work on some git requests (especially git fetch).
	// Besides, relying on git tool for such a simple but crucial functionality for our case
	// would be better for long term.
	cmd := exec.CommandContext(context.TODO(), "git", "upload-pack", "--stateless-rpc", KrewGitRepositoryPath)
	errbuf, outbuf := &bytes.Buffer{}, &bytes.Buffer{}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = r.Body, outbuf, io.MultiWriter(errbuf, os.Stderr)
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("endpoint failure: %s", err), http.StatusBadRequest)
		return
	}

	w.Header().Add("Content-Type", "application/x-git-upload-pack-result")
	w.Header().Add("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(outbuf.Bytes())
}

// pluginToKrewPlugin converts a Plugin to a Krew plugin.
func pluginToKrewPlugin(plugin configv1.Plugin, r *http.Request) ([]byte, error) {
	if len(plugin.Spec.Platforms) == 0 {
		return nil, fmt.Errorf("plugin does not have any platforms")
	}

	platforms := []Platform{}

	for _, plat := range plugin.Spec.Platforms {
		fields := strings.SplitN(plat.Platform, "/", 2)
		if len(fields) < 2 {
			continue
		}

		url := hostFromRequest(r) + fmt.Sprintf("/v1/plugins/download/?name=%s&platform=%s", plugin.Name, plat.Platform)

		p := Platform{
			URI: url,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"os":   fields[0],
					"arch": fields[1],
				},
			},
			Files: []FileOperation{},
			Bin:   plat.Bin,
		}

		for _, file := range plat.Files {
			p.Files = append(p.Files, FileOperation{
				From: file.From,
				To:   file.To,
			})
		}

		platforms = append(platforms, p)
	}

	kPlugin := Plugin{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "krew.googlecontainertools.github.com/v1alpha2",
			Kind:       "Plugin",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      plugin.Name,
			Namespace: plugin.Namespace,
		},
		Spec: PluginSpec{
			Version:          plugin.Spec.Version,
			ShortDescription: plugin.Spec.ShortDescription,
			Description:      plugin.Spec.Description,
			Caveats:          plugin.Spec.Caveats,
			Homepage:         plugin.Spec.Homepage,
			Platforms:        platforms,
		},
	}

	return yaml.Marshal(kPlugin)
}

func hostFromRequest(r *http.Request) string {
	if len(r.URL.Scheme) > 0 && len(r.URL.Host) > 0 {
		return r.URL.Scheme + "://" + r.URL.Host
	}

	proto := "https"
	if p := r.Header.Get("X-Forwarded-Proto"); len(p) > 0 {
		proto = p
	}

	host := "api-cli-manager.svc.local"
	if h := r.Header.Get("Host"); len(h) > 0 {
		host = h
	}

	return proto + "://" + host
}
