// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1

import (
	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/client-go/applyconfigurations/meta/v1"
)

// RequiredHSTSPolicyApplyConfiguration represents a declarative configuration of the RequiredHSTSPolicy type for use
// with apply.
type RequiredHSTSPolicyApplyConfiguration struct {
	NamespaceSelector       *metav1.LabelSelectorApplyConfiguration `json:"namespaceSelector,omitempty"`
	DomainPatterns          []string                                `json:"domainPatterns,omitempty"`
	MaxAge                  *MaxAgePolicyApplyConfiguration         `json:"maxAge,omitempty"`
	PreloadPolicy           *configv1.PreloadPolicy                 `json:"preloadPolicy,omitempty"`
	IncludeSubDomainsPolicy *configv1.IncludeSubDomainsPolicy       `json:"includeSubDomainsPolicy,omitempty"`
}

// RequiredHSTSPolicyApplyConfiguration constructs a declarative configuration of the RequiredHSTSPolicy type for use with
// apply.
func RequiredHSTSPolicy() *RequiredHSTSPolicyApplyConfiguration {
	return &RequiredHSTSPolicyApplyConfiguration{}
}

// WithNamespaceSelector sets the NamespaceSelector field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the NamespaceSelector field is set to the value of the last call.
func (b *RequiredHSTSPolicyApplyConfiguration) WithNamespaceSelector(value *metav1.LabelSelectorApplyConfiguration) *RequiredHSTSPolicyApplyConfiguration {
	b.NamespaceSelector = value
	return b
}

// WithDomainPatterns adds the given value to the DomainPatterns field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the DomainPatterns field.
func (b *RequiredHSTSPolicyApplyConfiguration) WithDomainPatterns(values ...string) *RequiredHSTSPolicyApplyConfiguration {
	for i := range values {
		b.DomainPatterns = append(b.DomainPatterns, values[i])
	}
	return b
}

// WithMaxAge sets the MaxAge field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the MaxAge field is set to the value of the last call.
func (b *RequiredHSTSPolicyApplyConfiguration) WithMaxAge(value *MaxAgePolicyApplyConfiguration) *RequiredHSTSPolicyApplyConfiguration {
	b.MaxAge = value
	return b
}

// WithPreloadPolicy sets the PreloadPolicy field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the PreloadPolicy field is set to the value of the last call.
func (b *RequiredHSTSPolicyApplyConfiguration) WithPreloadPolicy(value configv1.PreloadPolicy) *RequiredHSTSPolicyApplyConfiguration {
	b.PreloadPolicy = &value
	return b
}

// WithIncludeSubDomainsPolicy sets the IncludeSubDomainsPolicy field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the IncludeSubDomainsPolicy field is set to the value of the last call.
func (b *RequiredHSTSPolicyApplyConfiguration) WithIncludeSubDomainsPolicy(value configv1.IncludeSubDomainsPolicy) *RequiredHSTSPolicyApplyConfiguration {
	b.IncludeSubDomainsPolicy = &value
	return b
}
