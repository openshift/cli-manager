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

// PluginSpec defines the desired state of Plugin.
type PluginSpec struct {
	// ShortDescription of the plugin.
	// +required
	ShortDescription string `json:"shortDescription,omitempty"`

	// Description of the plugin.
	// +optional
	Description string `json:"description,omitempty"`

	// Caveats of using the plugin.
	// +optional
	Caveats string `json:"caveats,omitempty"`

	// Homepage of the plugin.
	// +optional
	Homepage string `json:"homepage,omitempty"`

	// Version of the plugin.
	// +required
	Version string `json:"versions,omitempty"`

	// Platforms the plugin supports.
	Platforms []PluginPlatform `json:"platforms,omitempty"`
}

// PluginPlatform defines per-OS and per-Arch binaries for the given plugin.
type PluginPlatform struct {
	// Platform for the given binary (i.e. linux/amd64, darwin/amd64, windows/amd64).
	// +required
	Platform string `json:"platform,omitempty"`

	// Image containing CLI tool.
	// +required
	Image string `json:"image,omitempty"`

	// ImagePullSecret to use when connecting to an image registry that requires authentication.
	// +optional
	ImagePullSecret string `json:"imagePullSecret,omitempty"`

	// Files is a list of file locations within the image that need to be extracted.
	// +required
	Files []FileOperation `json:"path,omitempty"`

	// Bin specifies the path to the plugin executable.
	// The path is relative to the root of the installation folder.
	// The binary will be linked after all FileOperations are executed.
	// +required
	Bin string `json:"bin,omitempty"`
}

// PluginFileOperation specifies a file copying operation from plugin archive to the
// installation directory.
type FileOperation struct {
	// From is the absolute file path within the image to copy from.
	// Directories and wildcards are not currently supported.
	// +required
	From string `json:"from,omitempty"`

	// To is the relative path within the root of the installation folder to place the file.
	// +required
	To string `json:"to,omitempty"`
}

// PluginStatus defines the observed state of Plugin.
type PluginStatus struct{}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Plugin is the Schema for the plugin API.
type Plugin struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PluginSpec   `json:"spec,omitempty"`
	Status PluginStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PluginList contains a list of Plugins.
type PluginList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Plugin `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Plugin{}, &PluginList{})
}
