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
