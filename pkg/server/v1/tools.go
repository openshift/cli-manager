package v1

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	configv1 "github.com/deejross/openshift-cli-manager/api/v1"
	"github.com/deejross/openshift-cli-manager/pkg/image"

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
			Namespace:     item.Namespace,
			Name:          item.Name,
			Description:   item.Spec.Description,
			LatestVersion: version.Version,
			Platforms:     platforms,
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
// If `version` is `latest`, returns the most recent known version.
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
			if v.Version == version || (version == "latest" && i == len(versions)-1) {
				out.Versions = append(out.Versions, v)
			}
		}
	}

	return out, nil
}

// DownloadTool downloads the given tool and writes it to the provided io.Writer.
// If `version` is an empty string, the most recent version is used.
// If `version` is not an empty string, the specified version is used.
func (v *V1) DownloadTool(namespace, name, platform, version string, w io.Writer) error {
	tool := &configv1.CLITool{}
	if err := v.cli.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, tool); err != nil {
		return err
	}

	// make sure CLITool has versions
	if tool.Spec.Versions == nil || len(tool.Spec.Versions) == 0 {
		return fmt.Errorf("misconfigured CLITool: name: %s/%s, error: there are no versions specified for the given CLITool", namespace, name)
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
		return fmt.Errorf("misconfigured CLITool: name: %s/%s, error: there are no binaries specified for the given CLITool version: %s", namespace, name, version)
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
		return &errors.StatusError{
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
			return fmt.Errorf("misconfigured CLITool: name: %s/%s, version: %s, platform: %s, error while getting imagePullSecret %s: %v", namespace, name, version, platform, binary.ImagePullSecret, err)
		}

		// ensure the Secret is of the expected type
		if imagePullSecret.Type != corev1.SecretTypeDockercfg {
			return fmt.Errorf("misconfigured CLITool: name: %s/%s, version: %s, platform: %s, error: configured imagePullSecret %s for given version and platform combination is not of type: %s", namespace, name, version, platform, binary.ImagePullSecret, corev1.SecretTypeDockercfg)
		}

		// set the .dockercfg auth information for the image puller
		pullOptions.Auth = string(imagePullSecret.Data[corev1.DockerConfigKey])
	}

	// attempt to pull the image down locally
	img, err := image.Pull(binary.Image, pullOptions)
	if err != nil {
		return fmt.Errorf("could not pull image: name: %s, error: %v for CLITool: name: %s/%s, version: %s, platform: %s", binary.Image, err, namespace, name, version, platform)
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

	// if a digest has not been caluculated yet, setup a TeeReader for hashing once the extract is finished
	var digestReader io.Reader
	if digestCalculated == 0 {
		buf := &bytes.Buffer{}
		digestReader = io.TeeReader(buf, w)
		w = buf
	}

	// configure the extractor based on the binary information, setting the output destination to the response body
	extractOptions := &image.ExtractOptions{
		Targets: []image.Target{
			{
				Source:      binary.Path,
				Destination: w,
			},
		},
	}

	// attempt to extract and write the raw binary to the body of the response
	if err := image.Extract(img, extractOptions); err != nil {
		return fmt.Errorf("unable to extract tool from image: name: %s/%s, version: %s, platform: %s, image: %s, path: %s, error: %v", namespace, name, version, binary.Platform, binary.Image, binary.Path, err)
	}

	// if digestReader was created, then we need to calculate the digest and update the CLITool's status
	if digestReader != nil {
		digest, err := v.CalculateDigest(digestReader)
		if err != nil {
			return fmt.Errorf("unable to calculate digest for binary: name: %s/%s, version: %s, platform: %s, image: %s, path: %s, error: %v", namespace, name, version, binary.Platform, binary.Image, binary.Path, err)
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

	return nil
}

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

	// set the appropriate response headers for downloading a binary
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Content-Transfer-Encoding", "binary")

	// get the requested CLITool resources
	err := v.DownloadTool(namespace, name, platform, version, w)
	if err != nil {
		if errors.IsNotFound(err) {
			v.respondUserError(w, 404, err)
			return
		}
		v.respondSystemError(w, 500, err, fmt.Sprintf("getting CLITool: name: %s/%s, platform: %s", namespace, name, platform))
		return
	}
}
