/*
Copyright 2022-2023 The Nephio Authors.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EdgeWatcher contains address and port of the edgewatcher GRPC server
type EdgeWatcher struct {
	Addr string `json:"addr,omitempty" yaml:"addr"`
	Port string `json:"port,omitempty" yaml:"port"`
}

// WatchRequest uniquely identifies a list of resources to watch
type WatchRequest struct {
	// +kubebuilder:validation:Enum=workload.nephio.org
	Group string `json:"group" yaml:"group"`
	// +kubebuilder:validation:Enum=v1alpha1
	Version string `json:"version" yaml:"version"`
	// +kubebuilder:validation:Enum=UPFDeployment;SMFDeployment
	Kind string `json:"kind" yaml:"kind"`
	// Namespace to restrict the watch, defaults to empty string which corresponds
	// to the "default" namespace
	Namespace string `json:"namespace" yaml:"namespace"`
}

// WatcherAgentSpec defines the desired state of WatcherAgent
type WatcherAgentSpec struct {
	// ResyncTimestamp is the time at which last resync was requested by
	// edgewatcher
	ResyncTimestamp metav1.Time    `json:"resyncTimestamp,omitempty" yaml:"resyncTimestamp"`
	EdgeWatcher     EdgeWatcher    `json:"edgeWatcher,omitempty" yaml:"edgeWatcher"`
	WatchRequests   []WatchRequest `json:"watchRequests" yaml:"watchRequests"`
}

// WatchRequestStatus specifies the last ResourceVersion sent to edgewatcher
type WatchRequestStatus struct {
	WatchRequest    `json:"watchRequest"`
	ResourceVersion string `json:"resourceVersion"`
}

// WatcherAgentStatus defines the observed state of WatcherAgent
type WatcherAgentStatus struct {
	// ResyncTimestamp is the timestamp of last successful resync request from
	// edgewatcher
	ResyncTimestamp metav1.Time          `json:"resyncTimestamp,omitempty"`
	WatchRequests   []WatchRequestStatus `json:"watchRequests,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// WatcherAgent is the Schema for the watcheragents API
type WatcherAgent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WatcherAgentSpec   `json:"spec,omitempty"`
	Status WatcherAgentStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// WatcherAgentList contains a list of WatcherAgent
type WatcherAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WatcherAgent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WatcherAgent{}, &WatcherAgentList{})
}
