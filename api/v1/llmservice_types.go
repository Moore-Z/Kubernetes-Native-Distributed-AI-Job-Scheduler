/*
Copyright 2026.

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

// LLMServiceSpec defines the desired state of LLMService
// Java Equivalent: public class LLMServiceSpec { String model; int replicas; String gpuMemory; }
type LLMServiceSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +kubebuilder:validation:Required
	// Model is the HuggingFace model ID, e.g., "deepseek-ai/deepseek-r1"
	Model string `json:"model"`

	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// Replicas is the number of vLLM pods to run
	Replicas int32 `json:"replicas,omitempty"`

	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	GpuPerReplica int32 `json:"gpuPerReplica,omitempty"`

	// +kubebuilder:default=none
	// +kubebuilder:validation:Enum=none;shared
	CacheStrategy string `json:"cacheStrategy,omitempty"`

	// +kubebuilder:default="vllm/vllm-openai:latest"
	Image string `json:"image,omitempty"`

	// +kubebuilder:validation:Pattern=`^\d+(Gi|Mi)$`
	// GPUMemory requirement, e.g. "24Gi". Used for scheduling.
	GPUMemory string `json:"gpuMemory,omitempty"`
}

// LLMServiceStatus defines the observed state of LLMService
type LLMServiceStatus struct {
	// AvailableReplicas is the number of pods currently running and ready
	AvailableReplicas int32 `json:"availableReplicas"`

	Conditions       []LLMServiceCondition `json:"conditions,omitempty"`
	CacheCoordinator string                `json:"cacheCoordinator,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// LLMService is the Schema for the llmservices API
type LLMService struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of LLMService
	// +required
	Spec LLMServiceSpec `json:"spec"`

	// status defines the observed state of LLMService
	// +optional
	Status LLMServiceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LLMServiceList contains a list of LLMService
type LLMServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LLMService `json:"items"`
}

type LLMServiceCondition struct {
	Type           string      `json:"type"`
	Status         string      `json:"status"`
	Reason         string      `json:"reason,omitempty"`
	Message        string      `json:"message,omitempty"`
	LastUpdateTime metav1.Time `json:"lastUpdateTime"`
}

func init() {
	SchemeBuilder.Register(&LLMService{}, &LLMServiceList{})
}
