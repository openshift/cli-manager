/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CLIToolSpec defines the desired state of CLITool.
type CLIToolSpec struct {
	// ShortDescription of the CLI tool.
	// +optional
	ShortDescription string `json:"shortDescription,omitempty"`

	// Description of the CLI tool.
	// +optional
	Description string `json:"description,omitempty"`

	// Caveats of using the CLI tool.
	// +optional
	Caveats string `json:"caveats,omitempty"`

	// Homepage of the CLI tool.
	// +optional
	Homepage string `json:"homepage,omitempty"`

	// Versions of the CLI tool.
	// +required
	Versions []CLIToolVersion `json:"versions,omitempty"`
}

// CLIToolVersion defines a version number for the tool.
type CLIToolVersion struct {
	// Version is the name or number of the version.
	// +required
	Version string `json:"version,omitempty"`

	// Binaries is a list of binaries for the given version.
	// +required
	Binaries []CLIToolVersionBinary `json:"binaries,omitempty"`
}

// CLIToolBinary defines per-OS and per-Arch binaries for the given tool.
type CLIToolVersionBinary struct {
	// Platform for the given binary (i.e. linux/amd64, darwin/amd64, windows/amd64).
	// +required
	Platform string `json:"platform,omitempty"`

	// Image containing CLI tool.
	// +required
	Image string `json:"image,omitempty"`

	// ImagePullSecret to use when connecting to an image registry that requires authentication.
	// +optional
	ImagePullSecret string `json:"imagePullSecret,omitempty"`

	// Path is the location within the image where the CLI tool can be found.
	// +required
	Path string `json:"path,omitempty"`
}

// CLIToolStatusDigest provides information about a hash for a tool's version/platform binary combination.
type CLIToolStatusDigest struct {
	// Name is the version/platform for the hash.
	Name string `json:"name,omitempty"`

	// Digest is the text representation of the hash.
	Digest string `json:"value,omitempty"`

	// Calculated is when the hash was calculated.
	Calculated metav1.Timestamp `json:"calculated,omitempty"`
}

// CLIToolStatus defines the observed state of CLITool
type CLIToolStatus struct {
	// Digests is a list of calculated hashes for a tool's version/platform combination.
	Digests []CLIToolStatusDigest `json:"hashes,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// CLITool is the Schema for the clitools API.
type CLITool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CLIToolSpec   `json:"spec,omitempty"`
	Status CLIToolStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CLIToolList contains a list of CLITool.
type CLIToolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CLITool `json:"items"`
}

// HTTPCLIToolListItem is the CLITool object represented by the controller's HTTP list endpoint.
type HTTPCLIToolListItem struct {
	Namespace        string   `json:"namespace"`
	Name             string   `json:"name"`
	ShortDescription string   `json:"shortDescription"`
	Description      string   `json:"description"`
	Caveats          string   `json:"caveats"`
	Homepage         string   `json:"homepage"`
	LatestVersion    string   `json:"latestVersion"`
	Platforms        []string `json:"platforms"`
}

// HTTPCLIToolList is the object returned by the controller's HTTP list endpoint.
type HTTPCLIToolList struct {
	Items []HTTPCLIToolListItem `json:"items"`
}

// HTTPCLIToolInfo is the detailed CLITool object represented by the controller's HTTP info endpoint.
type HTTPCLIToolInfo struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	CLIToolSpec
	CLIToolStatus
}

func init() {
	SchemeBuilder.Register(&CLITool{}, &CLIToolList{})
}
