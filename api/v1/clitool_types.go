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

// CLIToolSpec defines the desired state of CLITool
type CLIToolSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Description of the CLI tool
	// +optional
	Description string `json:"description,omitempty"`

	// Binaries for the CLI tool
	// +required
	Binaries []CLIToolBinary `json:"binaries,omitempty"`
}

// CLIToolBinary defines per-OS and per-Arch binaries for the given tool.
type CLIToolBinary struct {
	// OS is the operating system for the given binary (i.e. linux, darwin, windows)
	// +required
	OS string `json:"os,omitempty"`

	// Architecture is the CPU architecture for given binary (i.e. amd64, arm64)
	// +required
	Architecture string `json:"arch,omitempty"`

	// Image containing CLI tool
	// +required
	Image string `json:"image,omitempty"`

	// ImagePullSecret to use when connecting to an image registry that requires authentication
	// +optional
	ImagePullSecret string `json:"imagePullSecret,omitempty"`

	// Path is the location within the image where the CLI tool can be found
	// +required
	Path string `json:"path,omitempty"`
}

// CLIToolStatus defines the observed state of CLITool
type CLIToolStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// CLITool is the Schema for the clitools API
type CLITool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CLIToolSpec   `json:"spec,omitempty"`
	Status CLIToolStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CLIToolList contains a list of CLITool
type CLIToolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CLITool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CLITool{}, &CLIToolList{})
}
