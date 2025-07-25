// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1

import (
	metav1 "k8s.io/client-go/applyconfigurations/meta/v1"
)

// ImagePolicyStatusApplyConfiguration represents a declarative configuration of the ImagePolicyStatus type for use
// with apply.
type ImagePolicyStatusApplyConfiguration struct {
	Conditions []metav1.ConditionApplyConfiguration `json:"conditions,omitempty"`
}

// ImagePolicyStatusApplyConfiguration constructs a declarative configuration of the ImagePolicyStatus type for use with
// apply.
func ImagePolicyStatus() *ImagePolicyStatusApplyConfiguration {
	return &ImagePolicyStatusApplyConfiguration{}
}

// WithConditions adds the given value to the Conditions field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the Conditions field.
func (b *ImagePolicyStatusApplyConfiguration) WithConditions(values ...*metav1.ConditionApplyConfiguration) *ImagePolicyStatusApplyConfiguration {
	for i := range values {
		if values[i] == nil {
			panic("nil value passed to WithConditions")
		}
		b.Conditions = append(b.Conditions, *values[i])
	}
	return b
}
