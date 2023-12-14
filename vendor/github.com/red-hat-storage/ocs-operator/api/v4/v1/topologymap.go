/*
Copyright 2020 Red Hat OpenShift Container Storage.

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
	"strings"
)

// NewNodeTopologyMap returns an initialized NodeTopologyMap
func NewNodeTopologyMap() *NodeTopologyMap {
	return &NodeTopologyMap{
		Labels: map[string]TopologyLabelValues{},
	}
}

// Contains checks whether the NodeTopologyMap contains a specific value
// for the specified key
func (m *NodeTopologyMap) Contains(topologyKey string, value string) bool {
	if values, ok := m.Labels[topologyKey]; ok {
		for _, val := range values {
			if value == val {
				return true
			}
		}
	}

	return false
}

// ContainsKey checks whether the NodeTopologyMap contains any value for the
// specified key
func (m *NodeTopologyMap) ContainsKey(topologyKey string) bool {
	if _, ok := m.Labels[topologyKey]; ok {
		return true
	}

	return false
}

// Add adds a new value to the NodeTopologyMap under the specified key
// USe it with Contains() to not allow duplicate values
func (m *NodeTopologyMap) Add(topologyKey string, value string) {
	if _, ok := m.Labels[topologyKey]; !ok {
		m.Labels[topologyKey] = TopologyLabelValues{}
	}

	m.Labels[topologyKey] = append(m.Labels[topologyKey], value)
}

// GetKeyValues returns a node label matching the topologyKey and all values
// for that label across all storage nodes
func (m *NodeTopologyMap) GetKeyValues(topologyKey string) (string, []string) {
	values := []string{}

	for label, labelValues := range m.Labels {
		if strings.Contains(label, topologyKey) {
			topologyKey = label
			values = labelValues
			break
		}
	}

	return topologyKey, values
}
