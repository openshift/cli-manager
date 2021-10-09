package v1

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
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
	"k8s.io/client-go/util/retry"
)

// ListTools returns a list of available tools.
// If `platform` is an empty string, returns all tools regardless of supported platform.
// if `platform` is a non-empty string, only returns tools supported by the given platform.
func (v *V1) ListTools(platform string) (*configv1.HTTPCLIToolList, error) {
	// get list of CLITools
	list := &configv1.CLIToolList{}
	if err := v.cli.List(context.Background(), list); err != nil {
		return nil, fmt.Errorf("obtaining list of tools from k8s API: %v", err)
	}

	// sort the output by tool name
	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].Name < list.Items[j].Name
	})

	// build output object
	out := &configv1.HTTPCLIToolList{Items: []configv1.HTTPCLIToolListItem{}}
	for _, item := range list.Items {
		if item.Spec.Versions == nil || len(item.Spec.Versions) == 0 {
			continue
		}

		version := item.Spec.Versions[len(item.Spec.Versions)-1]
		platforms := []string{}
		for _, bin := range version.Binaries {
			platforms = append(platforms, bin.Platform)
		}

		outItem := configv1.HTTPCLIToolListItem{
			Namespace:        item.Namespace,
			Name:             item.Name,
			ShortDescription: item.Spec.ShortDescription,
			Description:      item.Spec.Description,
			Caveats:          item.Spec.Caveats,
			Homepage:         item.Spec.Homepage,
			LatestVersion:    version.Version,
			Platforms:        platforms,
		}

		if len(platform) > 0 {
			for _, p := range platforms {
				if p == platform {
					out.Items = append(out.Items, outItem)
					break
				}
			}
			continue
		}

		out.Items = append(out.Items, outItem)
	}

	return out, nil
}

// ToolInfo returns information about a tool.
// If `version` is an empty string, returns all known versions.
// If `version` is a non-empty string, returns only information about the given version.
func (v *V1) ToolInfo(namespace, name, version string) (*configv1.HTTPCLIToolInfo, error) {
	// get the requested CLITool resources
	tool := &configv1.CLITool{}
	if err := v.cli.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, tool); err != nil {
		return nil, err
	}

	out := &configv1.HTTPCLIToolInfo{
		Namespace:     tool.Namespace,
		Name:          tool.Name,
		CLIToolSpec:   tool.Spec,
		CLIToolStatus: tool.Status,
	}

	if len(version) > 0 {
		versions := out.Versions
		out.Versions = []configv1.CLIToolVersion{}
		for i, v := range versions {
			if v.Version == version || i == len(versions)-1 {
				out.Versions = append(out.Versions, v)
			}
		}
	}

	return out, nil
}

// ToolDigest returns the digest of a tool's version and platform.
// If `version` is empty the most recent known version is used.
func (v *V1) ToolDigest(namespace, name, platform, version string) (string, error) {
	reader, err := v.GetBinaryFromImage(namespace, name, platform, version)
	if err != nil {
		return "", err
	}

	return v.CalculateDigest(reader)
}

// ToolInfoFromDigest attempts to return information about a tool from the given digest.
// If there are multiple tools with the same digest, only the first one found is returned.
// Returns nil if nothing is found.
func (v *V1) ToolInfoFromDigest(digest string) (*configv1.HTTPCLIToolInfo, error) {
	// get list of CLITools
	list := &configv1.CLIToolList{}
	if err := v.cli.List(context.Background(), list); err != nil {
		return nil, fmt.Errorf("obtaining list of tools from k8s API: %v", err)
	}

	// loop through items
	for _, tool := range list.Items {
		if tool.Status.Digests == nil || len(tool.Status.Digests) == 0 {
			continue
		}

		// loop through digests
		newDigests := []configv1.CLIToolStatusDigest{}
		newVersions := []configv1.CLIToolVersion{}
		for _, d := range tool.Status.Digests {
			if d.Digest == digest {
				fields := strings.SplitN(d.Name, "/", 2)
				if len(fields) < 2 {
					continue
				}

				// loop through versions to find which one applies to this digest
				version := fields[0]
				for _, v := range tool.Spec.Versions {
					if v.Version == version {
						newDigests = append(newDigests, d)
						newVersions = append(newVersions, v)
					}
				}
			}
		}

		if len(newVersions) > 0 {
			tool.Spec.Versions = newVersions
			tool.Status.Digests = newDigests

			return &configv1.HTTPCLIToolInfo{
				Namespace:     tool.Namespace,
				Name:          tool.Name,
				CLIToolSpec:   tool.Spec,
				CLIToolStatus: tool.Status,
			}, nil
		}
	}

	return nil, nil
}

// DownloadTool downloads the given tool and writes it to the provided io.Writer.
// If `version` is empty, the most recent version is used.
func (v *V1) DownloadTool(namespace, name, platform, version string, w io.Writer) error {
	reader, err := v.GetBinaryFromImage(namespace, name, platform, version)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, reader)
	return err
}

// GetBinaryFromImage gets the binary from the named tool's platform and version.
// If `version` is empty, the most recent version is used.
func (v *V1) GetBinaryFromImage(namespace, name, platform, version string) (io.Reader, error) {
	tool := &configv1.CLITool{}
	if err := v.cli.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, tool); err != nil {
		return nil, err
	}

	// make sure CLITool has versions
	if tool.Spec.Versions == nil || len(tool.Spec.Versions) == 0 {
		return nil, fmt.Errorf("misconfigured CLITool: name: %s/%s, error: there are no versions specified for the given CLITool", namespace, name)
	}

	// use latest version if not specified
	if len(version) == 0 {
		version = tool.Spec.Versions[len(tool.Spec.Versions)-1].Version
	}

	// get binaries list for given version
	var binaries []configv1.CLIToolVersionBinary
	for _, ver := range tool.Spec.Versions {
		if ver.Version == version {
			binaries = ver.Binaries
		}
	}

	// make sure there are binaries within the CLITool resource
	if len(binaries) == 0 {
		return nil, fmt.Errorf("misconfigured CLITool: name: %s/%s, error: there are no binaries specified for the given CLITool version: %s", namespace, name, version)
	}

	// find the correct binary for the given operating system and architecture combination
	var binary *configv1.CLIToolVersionBinary
	for _, bin := range binaries {
		if bin.Platform == platform {
			binary = &bin
			break
		}
	}

	// return an error if there is no binary for the given operating system and architecture combination
	if binary == nil {
		// we return this type of error instead of using `fmt.Errorf` so that `errors.IsNotFound(err)` will return true, and allow an HTTP handler to return a 404 status code
		return nil, &errors.StatusError{
			ErrStatus: metav1.Status{
				Status:  metav1.StatusFailure,
				Code:    http.StatusNotFound,
				Reason:  metav1.StatusReasonNotFound,
				Details: &metav1.StatusDetails{},
				Message: fmt.Sprintf("desired CLITool does not have a binary for the requested version and platform combination: name: %s/%s, version: %s, platform: %s", namespace, name, version, platform),
			},
		}
	}

	// start configuring the image puller
	pullOptions := &image.PullOptions{}
	if len(binary.ImagePullSecret) > 0 {
		// if an imagePullSecret is defined for the binary, retrieve the Secret for it
		imagePullSecret := &corev1.Secret{}
		if err := v.cli.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: binary.ImagePullSecret}, imagePullSecret); err != nil {
			return nil, fmt.Errorf("misconfigured CLITool: name: %s/%s, version: %s, platform: %s, error while getting imagePullSecret %s: %v", namespace, name, version, platform, binary.ImagePullSecret, err)
		}

		// ensure the Secret is of the expected type
		if imagePullSecret.Type != corev1.SecretTypeDockercfg {
			return nil, fmt.Errorf("misconfigured CLITool: name: %s/%s, version: %s, platform: %s, error: configured imagePullSecret %s for given version and platform combination is not of type: %s", namespace, name, version, platform, binary.ImagePullSecret, corev1.SecretTypeDockercfg)
		}

		// set the .dockercfg auth information for the image puller
		pullOptions.Auth = string(imagePullSecret.Data[corev1.DockerConfigKey])
	}

	// attempt to pull the image down locally
	img, err := image.Pull(binary.Image, pullOptions)
	if err != nil {
		return nil, fmt.Errorf("could not pull image: name: %s, error: %v for CLITool: name: %s/%s, version: %s, platform: %s", binary.Image, err, namespace, name, version, platform)
	}

	// check to see if a digest has been calculated for this binary
	digestCalculated := int64(0)
	digestName := fmt.Sprintf("%s/%s", version, platform)
	for _, d := range tool.Status.Digests {
		if d.Name == digestName {
			digestCalculated = d.Calculated.Seconds
			break
		}
	}

	// create a buffer for the binary contents
	toolBuf := &bytes.Buffer{}

	// if a digest has not been calculated yet, setup a TeeReader for hashing once the extract is finished
	var digestReader io.Reader
	if digestCalculated == 0 {
		buf := &bytes.Buffer{}
		digestReader = io.TeeReader(buf, toolBuf)
		toolBuf = buf
	}

	// configure the extractor based on the binary information, setting the output destination to the response body
	extractOptions := &image.ExtractOptions{
		Targets: []image.Target{
			{
				Source:      binary.Path,
				Destination: toolBuf,
			},
		},
	}

	// attempt to extract and write the raw binary to the body of the response
	if err := image.Extract(img, extractOptions); err != nil {
		return nil, fmt.Errorf("unable to extract tool from image: name: %s/%s, version: %s, platform: %s, image: %s, path: %s, error: %v", namespace, name, version, binary.Platform, binary.Image, binary.Path, err)
	}

	// if digestReader was created, then we need to calculate the digest and update the CLITool's status
	if digestReader != nil {
		digest, err := v.CalculateDigest(digestReader)
		if err != nil {
			return nil, fmt.Errorf("unable to calculate digest for binary: name: %s/%s, version: %s, platform: %s, image: %s, path: %s, error: %v", namespace, name, version, binary.Platform, binary.Image, binary.Path, err)
		}

		err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			tool := &configv1.CLITool{}
			if err := v.cli.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, tool); err != nil {
				return err
			}

			if tool.Status.Digests == nil {
				tool.Status.Digests = []configv1.CLIToolStatusDigest{}
			}

			tool.Status.Digests = append(tool.Status.Digests, configv1.CLIToolStatusDigest{
				Name:       digestName,
				Digest:     digest,
				Calculated: metav1.Timestamp{Seconds: time.Now().Unix()},
			})

			return v.cli.Status().Update(context.Background(), tool)
		})
		if err != nil {
			v.log.Error(err, fmt.Sprintf("attempting to update CLITool.Status.Digest with new digest: name: %s/%s", namespace, name))
		}
	}

	return toolBuf, nil
}

// CalculateDigest calculates the SHA256 digest of the given stream.
func (v *V1) CalculateDigest(r io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, r); err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), nil
}

func (v *V1) handleListTools(w http.ResponseWriter, r *http.Request) {
	// validate user input
	if r.Method != "GET" && r.Method != "LIST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// get optional fields
	platform := r.URL.Query().Get("platform")

	out, err := v.ListTools(platform)
	if err != nil {
		v.respondSystemError(w, 500, err, "listing tools")
		return
	}

	// output list as JSON
	v.respondJSON(w, out)
}

func (v *V1) handleToolInfo(w http.ResponseWriter, r *http.Request) {
	// validate user input
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// handle digest query
	digest := r.URL.Query().Get("digest")
	if len(digest) > 0 {
		out, err := v.ToolInfoFromDigest(digest)
		if err != nil {
			v.respondSystemError(w, 500, err, fmt.Sprintf("getting CLITool: digest: %s", digest))
			return
		}

		if out == nil {
			v.respondUserError(w, 404, fmt.Errorf("CLITool not found for given digest"))
			return
		}

		v.respondJSON(w, out)
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

	// get optional fields
	version := r.URL.Query().Get("version")

	out, err := v.ToolInfo(namespace, name, version)
	if err != nil {
		if errors.IsNotFound(err) {
			v.respondUserError(w, 404, err)
			return
		}
		v.respondSystemError(w, 500, err, fmt.Sprintf("getting CLITool: name: %s/%s", namespace, name))
		return
	}

	// output info as JSON
	v.respondJSON(w, out)
}

func (v *V1) handleDownloadTool(w http.ResponseWriter, r *http.Request) {
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

	version := r.URL.Query().Get("version")

	// if operatingSystem is `windows`, append `.exe` to the resulting binary name to improve download experience for Windows users
	filename := name
	if strings.HasPrefix(platform, "windows/") {
		filename += ".exe"
	}

	// set the requested output format
	format := r.URL.Query().Get("format")
	var writer io.Writer

	switch format {
	case "", "raw":
		writer = w
	case "zip":
		filename += "." + format

		z := zip.NewWriter(w)
		defer z.Close()

		var err error
		writer, err = z.Create(name)
		if err != nil {
			v.respondSystemError(w, 500, err, "generating zip")
			return
		}
	default:
		v.respondUserError(w, 400, fmt.Errorf("unknown format: %s", format))
		return
	}

	// set the appropriate response headers for downloading a binary
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Content-Transfer-Encoding", "binary")

	// get the requested CLITool resources
	err := v.DownloadTool(namespace, name, platform, version, writer)
	if err != nil {
		if errors.IsNotFound(err) {
			v.respondUserError(w, 404, err)
			return
		}
		v.respondSystemError(w, 500, err, fmt.Sprintf("getting CLITool: name: %s/%s, platform: %s", namespace, name, platform))
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

// buildGitRepo builds a git repo from the list of configured tools.
func (v *V1) buildGitRepo(repoName string, r *http.Request) (string, *git.Repository, *git.Worktree, error) {
	// TODO: temp list of tools, replace with actual CLITools
	tools := &configv1.CLIToolList{
		Items: []configv1.CLITool{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bash",
					Namespace: "default",
				},
				Spec: configv1.CLIToolSpec{
					Description: "just a test",
					Versions: []configv1.CLIToolVersion{
						{
							Version: "v4.4.20",
							Binaries: []configv1.CLIToolVersionBinary{
								{
									Platform: "linux/amd64",
									Image:    "redhat/ubi8-micro:latest",
									Path:     "/usr/bin/bash",
								},
							},
						},
					},
				},
			},
		},
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

	for _, tool := range tools.Items {
		name := filepath.Join(dir, fmt.Sprintf("%s-%s.yaml", tool.ObjectMeta.Namespace, tool.ObjectMeta.Name))
		f, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0644)
		if err != nil {
			return "", nil, nil, err
		}

		y, err := v.toolToKrewPlugin(tool, r)
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

// toolToKrewPlugin converts a tool to a Krew plugin.
func (v *V1) toolToKrewPlugin(tool configv1.CLITool, r *http.Request) ([]byte, error) {
	if len(tool.Spec.Versions) == 0 {
		return nil, fmt.Errorf("tool does not have any versions")
	}

	version := tool.Spec.Versions[len(tool.Spec.Versions)-1]
	platforms := []Platform{}

	for _, bin := range version.Binaries {
		fields := strings.SplitN(bin.Platform, "/", 2)
		if len(fields) < 2 {
			continue
		}

		digest, err := v.ToolDigest(tool.Namespace, tool.Name, bin.Platform, version.Version)
		if err != nil {
			return nil, err
		}

		url := hostFromRequest(r) + fmt.Sprintf("/v1/tools/download/?namespace=%s&name=%s&platform=%s&version=%s&format=zip", tool.Namespace, tool.Name, bin.Platform, version.Version)

		p := Platform{
			URI:    url,
			Sha256: strings.TrimPrefix(digest, "sha256:"),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"os":   fields[0],
					"arch": fields[1],
				},
			},
			Files: []FileOperation{
				{
					From: tool.Name,
					To:   ".",
				},
			},
			Bin: tool.Name,
		}

		platforms = append(platforms, p)
	}

	plugin := Plugin{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "krew.googlecontainertools.github.com/v1alpha2",
			Kind:       "Plugin",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: tool.Name,
		},
		Spec: PluginSpec{
			Version:          version.Version,
			ShortDescription: tool.Spec.ShortDescription,
			Description:      tool.Spec.Description,
			Caveats:          tool.Spec.Caveats,
			Homepage:         tool.Spec.Homepage,
			Platforms:        platforms,
		},
	}

	return yaml.Marshal(plugin)
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
