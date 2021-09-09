package v1

import (
	"context"
	"fmt"
	"net/http"

	configv1 "github.com/deejross/openshift-cli-manager/api/v1"
	"github.com/deejross/openshift-cli-manager/pkg/image"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

func (v *V1) listTools(w http.ResponseWriter, r *http.Request) {
	// validate user input
	if r.Method != "GET" && r.Method != "LIST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// get list of CLITools
	list := &configv1.CLIToolList{}
	if err := v.cli.List(context.Background(), list); err != nil {
		v.respondSystemError(w, 500, err, "obtaining list of tools from k8s API")
		return
	}

	// output list as JSON
	v.respondJSON(w, list)
}

func (v *V1) downloadTool(w http.ResponseWriter, r *http.Request) {
	// validate user input
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get("name")
	if len(name) == 0 {
		v.respondUserError(w, 400, fmt.Errorf("missing name in query"))
		return
	}

	operatingSystem := r.URL.Query().Get("os")
	if len(operatingSystem) == 0 {
		v.respondUserError(w, 400, fmt.Errorf("missing os in query"))
		return
	}

	architecture := r.URL.Query().Get("arch")
	if len(architecture) == 0 {
		v.respondUserError(w, 400, fmt.Errorf("missing arch in query"))
		return
	}

	// get the requested CLITool resources
	tool := &configv1.CLITool{}
	if err := v.cli.Get(context.Background(), types.NamespacedName{Name: name}, tool); err != nil {
		if errors.IsNotFound(err) {
			v.respondUserError(w, 404, err)
			return
		}
		v.respondSystemError(w, 500, err, fmt.Sprintf("getting CLITool: name: %s, os: %s, arch: %s", name, operatingSystem, architecture))
		return
	}

	// make sure there are binaries within the CLITool resource
	if tool.Spec.Binaries == nil || len(tool.Spec.Binaries) == 0 {
		v.respondSystemError(w, 500, fmt.Errorf("misconfigured CLITool: name: %s, error: there are no binaries specified for the given CLITool", name), "validating CLITool binaries")
		return
	}

	// find the correct binary for the given operating system and architecture combination
	var binary *configv1.CLIToolBinary
	for _, bin := range tool.Spec.Binaries {
		if bin.Architecture == architecture && bin.OS == operatingSystem {
			binary = &bin
			break
		}
	}

	// return an error if there is no binary for the given operating system and architecture combination
	if binary == nil {
		v.respondUserError(w, 404, fmt.Errorf("desired CLITool does not have a binary for the requested os and arch combination: name: %s, os: %s, arch: %s", name, operatingSystem, architecture))
		return
	}

	// start configuring the image puller
	pullOptions := &image.PullOptions{}
	if len(binary.ImagePullSecret) > 0 {
		// if an imagePullSecret is defined for the binary, retrieve the Secret for it
		imagePullSecret := &corev1.Secret{}
		if err := v.cli.Get(context.Background(), types.NamespacedName{Name: binary.ImagePullSecret}, imagePullSecret); err != nil {
			v.respondSystemError(w, 500, fmt.Errorf("misconfigured CLITool: name: %s, os: %s, arch: %s, error: %v", name, operatingSystem, architecture, err), "getting imagePullSecret: name: "+binary.ImagePullSecret)
			return
		}

		// ensure the Secret is of the expected type
		if imagePullSecret.Type != corev1.SecretTypeDockercfg {
			v.respondSystemError(w, 500, fmt.Errorf("misconfigured CLITool: name: %s, os: %s, arch: %s, error: configured imagePullSecret for give os and arch combination is not of type: %s", name, operatingSystem, architecture, corev1.SecretTypeDockercfg), "getting imagePullSecret: name: "+binary.ImagePullSecret)
			return
		}

		// set the .dockercfg auth information for the image puller
		pullOptions.Auth = string(imagePullSecret.Data[corev1.DockerConfigKey])
	}

	// attempt to pull the image down locally
	img, err := image.Pull(binary.Image, pullOptions)
	if err != nil {
		v.respondSystemError(w, 500, fmt.Errorf("could not pull image: name: %s, error: %v", binary.Image, err), fmt.Sprintf("pulling image for CLITool: name: %s, os: %s, arch: %s", name, operatingSystem, architecture))
		return
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

	// if operatingSystem is `windows`, append `.exe` to the resulting binary name to improve download experience for Windows users
	filename := tool.Name
	if binary.OS == "windows" {
		filename += ".exe"
	}

	// set the appropriate response headers for downloading a binary
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Content-Transfer-Encoding", "binary")

	// attempt to extract and write the raw binary to the body of the response
	if err := image.Extract(img, extractOptions); err != nil {
		v.respondSystemError(w, 500, fmt.Errorf("unable to extract tool from image: name: %s, image: %s, os: %s, arch: %s, path: %s, error: %v", name, binary.Image, operatingSystem, architecture, binary.Path, err), "extracting tool from image")
		return
	}
}
