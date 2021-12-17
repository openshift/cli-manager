package v1

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	configv1 "github.com/deejross/openshift-cli-manager/api/v1"
	"github.com/deejross/openshift-cli-manager/pkg/image"
	"sigs.k8s.io/yaml"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ListPlugins returns a list of available plugins.
// If `platform` is an empty string, returns all plugins regardless of supported platform.
// if `platform` is a non-empty string, only returns plugins supported by the given platform.
func (v *V1) ListPlugins(platform string) (*configv1.PluginList, error) {
	// get list of Plugins
	list := &configv1.PluginList{}
	if err := v.cli.List(context.Background(), list); err != nil {
		return nil, fmt.Errorf("obtaining list of plugins from k8s API: %v", err)
	}

	// sort the output by plugin name
	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].Name < list.Items[j].Name
	})

	// filter output object by platform
	out := &configv1.PluginList{Items: []configv1.Plugin{}}
	for _, item := range list.Items {
		platforms := []string{}
		for _, bin := range item.Spec.Platforms {
			platforms = append(platforms, bin.Platform)
		}

		if len(platform) > 0 {
			for _, p := range platforms {
				if p == platform {
					out.Items = append(out.Items, item)
					break
				}
			}
			continue
		}

		out.Items = append(out.Items, item)
	}

	return out, nil
}

// PluginInfo returns information about a plugin.
func (v *V1) PluginInfo(namespace, name string) (*configv1.Plugin, error) {
	// get the requested Plugin resources
	plugin := &configv1.Plugin{}
	if err := v.cli.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, plugin); err != nil {
		return nil, err
	}

	return plugin, nil
}

// DownloadPlugin downloads the given plugin and writes it to the provided io.Writer.
func (v *V1) DownloadPlugin(namespace, name, platform string, w io.Writer) error {
	reader, err := v.GetBinariesFromImage(namespace, name, platform)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, reader)
	return err
}

// GetBinariesFromImage gets the binaries from the named plugin's platform.
// The returned `io.Reader` is a tar.gz archive of the binaries.
func (v *V1) GetBinariesFromImage(namespace, name, platform string) (io.Reader, error) {
	tool := &configv1.Plugin{}
	if err := v.cli.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, tool); err != nil {
		return nil, err
	}

	// make sure Plugin has platforms
	if len(tool.Spec.Platforms) == 0 {
		return nil, fmt.Errorf("misconfigured Plugin: name: %s/%s, error: there are no platforms specified for the given Plugin", namespace, name)
	}

	// get binaries list for given platform
	var pluginPlatform *configv1.PluginPlatform
	for _, plat := range tool.Spec.Platforms {
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
				Message: fmt.Sprintf("desired Plugin does not have a binary for the requested platform combination: name: %s/%s, platform: %s", namespace, name, platform),
			},
		}
	}

	// start configuring the image puller
	pullOptions := &image.PullOptions{}
	if len(pluginPlatform.ImagePullSecret) > 0 {
		// if an imagePullSecret is defined for the binary, retrieve the Secret for it
		imagePullSecret := &corev1.Secret{}
		if err := v.cli.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: pluginPlatform.ImagePullSecret}, imagePullSecret); err != nil {
			return nil, fmt.Errorf("misconfigured Plugin: name: %s/%s, platform: %s, error while getting imagePullSecret %s: %v", namespace, name, platform, pluginPlatform.ImagePullSecret, err)
		}

		// ensure the Secret is of the expected type
		if imagePullSecret.Type != corev1.SecretTypeDockercfg {
			return nil, fmt.Errorf("misconfigured Plugin: name: %s/%s, platform: %s, error: configured imagePullSecret %s for given platform combination is not of type: %s", namespace, name, platform, pluginPlatform.ImagePullSecret, corev1.SecretTypeDockercfg)
		}

		// set the .dockercfg auth information for the image puller
		pullOptions.Auth = string(imagePullSecret.Data[corev1.DockerConfigKey])
	}

	// attempt to pull the image down locally
	img, err := image.Pull(pluginPlatform.Image, pullOptions)
	if err != nil {
		return nil, fmt.Errorf("could not pull image: name: %s, error: %v for Plugin: name: %s/%s, platform: %s", pluginPlatform.Image, err, namespace, name, platform)
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
		return nil, fmt.Errorf("unable to extract binary from image: name: %s/%s, platform: %s, image: %s, error: %v", namespace, name, pluginPlatform.Platform, pluginPlatform.Image, err)
	}

	return extractOptions.Destination.(*bytes.Buffer), nil
}

func (v *V1) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	// validate user input
	if r.Method != "GET" && r.Method != "LIST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// get optional fields
	platform := r.URL.Query().Get("platform")

	out, err := v.ListPlugins(platform)
	if err != nil {
		v.respondSystemError(w, 500, err, "listing plugins")
		return
	}

	// output list as JSON
	v.respondJSON(w, out)
}

func (v *V1) handlePluginInfo(w http.ResponseWriter, r *http.Request) {
	// validate user input
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// handle namespace/name query
	namespace := r.URL.Query().Get("namespace")
	if len(namespace) == 0 {
		v.respondUserError(w, 400, fmt.Errorf("missing namespace in query"))
		return
	}

	name := r.URL.Query().Get("name")
	if len(name) == 0 {
		v.respondUserError(w, 400, fmt.Errorf("missing name in query"))
		return
	}

	out, err := v.PluginInfo(namespace, name)
	if err != nil {
		if errors.IsNotFound(err) {
			v.respondUserError(w, 404, err)
			return
		}
		v.respondSystemError(w, 500, err, fmt.Sprintf("getting Plugin: name: %s/%s", namespace, name))
		return
	}

	// output info as JSON
	v.respondJSON(w, out)
}

func (v *V1) handleDownloadPlugin(w http.ResponseWriter, r *http.Request) {
	// validate user input
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	namespace := r.URL.Query().Get("namespace")
	if len(namespace) == 0 {
		v.respondUserError(w, 400, fmt.Errorf("missing namespace in query"))
		return
	}

	name := r.URL.Query().Get("name")
	if len(name) == 0 {
		v.respondUserError(w, 400, fmt.Errorf("missing name in query"))
		return
	}

	platform := r.URL.Query().Get("platform")
	if len(platform) == 0 {
		v.respondUserError(w, 400, fmt.Errorf("missing platform in query"))
		return
	}

	filename := name + ".tar.gz"

	// set the appropriate response headers for downloading a binary
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Content-Transfer-Encoding", "binary")

	// get the requested Plugin resources
	err := v.DownloadPlugin(namespace, name, platform, w)
	if err != nil {
		if errors.IsNotFound(err) {
			v.respondUserError(w, 404, err)
			return
		}
		v.respondSystemError(w, 500, err, fmt.Sprintf("getting Plugin: name: %s/%s, platform: %s", namespace, name, platform))
		return
	}
}

func (v *V1) handleGitRequests(w http.ResponseWriter, r *http.Request) {
	paths := strings.SplitN(strings.TrimPrefix(r.URL.String(), "/v1/"), "/", 2)

	if len(paths) < 2 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	repo := paths[0]
	path := strings.TrimSuffix(paths[1], "/")

	switch path {
	case "info/refs?service=git-upload-pack":
		v.handleGitUploadPackAdvertisement(repo, path, w, r)
	case "git-upload-pack":
		v.handleGitUploadPackResult(repo, path, w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (v *V1) handleGitUploadPackAdvertisement(repoName, path string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dir, _, tree, err := v.buildGitRepo(repoName, r)
	if err != nil {
		v.log.Error(err, "buildGitRepo")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(dir)

	endpoint, err := transport.NewEndpoint(".git")
	if err != nil {
		http.Error(w, fmt.Sprintf("endpoint failure: %s", err), http.StatusBadRequest)
		return
	}

	loader := server.NewFilesystemLoader(tree.Filesystem)
	srv := server.NewServer(loader)
	session, err := srv.NewUploadPackSession(endpoint, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("starting session: %s", err), http.StatusInternalServerError)
		return
	}

	ar, err := session.AdvertisedReferences()
	if err != nil {
		http.Error(w, fmt.Sprintf("getting advertised references: %s", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("001e# service=git-upload-pack\n0000"))

	if err := ar.Encode(w); err != nil {
		http.Error(w, fmt.Sprintf("encoding server response: %s", err), http.StatusInternalServerError)
		return
	}
}

func (v *V1) handleGitUploadPackResult(repoName, path string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dir, _, tree, err := v.buildGitRepo(repoName, r)
	if err != nil {
		v.log.Error(err, "buildGitRepo")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(dir)

	endpoint, err := transport.NewEndpoint(".git")
	if err != nil {
		http.Error(w, fmt.Sprintf("endpoint failure: %s", err), http.StatusBadRequest)
		return
	}

	loader := server.NewFilesystemLoader(tree.Filesystem)
	srv := server.NewServer(loader)
	session, err := srv.NewUploadPackSession(endpoint, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("starting session: %s", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	w.WriteHeader(http.StatusOK)

	body := r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		var err error
		body, err = gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	req := packp.NewUploadPackRequest()
	if err := req.Decode(body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var resp *packp.UploadPackResponse
	resp, err = session.UploadPack(context.TODO(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := resp.Encode(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// buildGitRepo builds a git repo from the list of configured plugins.
func (v *V1) buildGitRepo(repoName string, r *http.Request) (string, *git.Repository, *git.Worktree, error) {
	plugins := &configv1.PluginList{}
	if err := v.cli.List(context.Background(), plugins); err != nil {
		return "", nil, nil, fmt.Errorf("unable to list plugins: %w", err)
	}

	dir, err := ioutil.TempDir("", "init")
	if err != nil {
		return "", nil, nil, fmt.Errorf("unable to create temporary directory: %w", err)
	}

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		return "", nil, nil, fmt.Errorf("could not init repo: %w", err)
	}

	tree, err := repo.Worktree()
	if err != nil {
		return "", nil, nil, err
	}

	for _, plugin := range plugins.Items {
		name := filepath.Join(dir, fmt.Sprintf("%s-%s.yaml", plugin.ObjectMeta.Namespace, plugin.ObjectMeta.Name))
		f, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0644)
		if err != nil {
			return "", nil, nil, err
		}

		y, err := v.pluginToKrewPlugin(plugin, r)
		if err != nil {
			f.Close()
			return "", nil, nil, err
		}

		f.Write(y)
		f.Close()
	}

	if err := tree.AddGlob("."); err != nil {
		return "", nil, nil, err
	}

	if _, err := tree.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "OpenShift CLI Manager",
			Email: "info@redhat.com",
			When:  time.Now(),
		},
	}); err != nil {
		return "", nil, nil, err
	}

	if err := repo.CreateBranch(&config.Branch{
		Name: string(plumbing.Master),
	}); err != nil {
		return "", nil, nil, fmt.Errorf("could not create %s branch: %w", plumbing.Master, err)
	}

	return dir, repo, tree, nil
}

// pluginToKrewPlugin converts a Plugin to a Krew plugin.
func (v *V1) pluginToKrewPlugin(plugin configv1.Plugin, r *http.Request) ([]byte, error) {
	if len(plugin.Spec.Platforms) == 0 {
		return nil, fmt.Errorf("plugin does not have any platforms")
	}

	platforms := []Platform{}

	for _, plat := range plugin.Spec.Platforms {
		fields := strings.SplitN(plat.Platform, "/", 2)
		if len(fields) < 2 {
			continue
		}

		url := hostFromRequest(r) + fmt.Sprintf("/v1/plugins/download/?namespace=%s&name=%s&platform=%s", plugin.Namespace, plugin.Name, plat.Platform)

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

	host := "api-openshift-cli-manager.svc.local"
	if h := r.Header.Get("Host"); len(h) > 0 {
		host = h
	}

	return proto + "://" + host
}
