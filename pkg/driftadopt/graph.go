package driftadopt

import (
	"encoding/json"
	"fmt"
)

// Graph represents a dependency graph of Pulumi resources
type Graph struct {
	// Nodes maps URN to Node
	Nodes map[string]*Node

	// Edges maps URN to list of dependent URNs
	Edges map[string][]string
}

// Node represents a resource in the dependency graph
type Node struct {
	// URN is the Pulumi URN of the resource
	URN string

	// Type is the resource type
	Type string

	// Dependencies are the URNs this resource depends on
	Dependencies []string

	// Dependents are the URNs that depend on this resource
	Dependents []string
}

// pulumiState represents the structure of a Pulumi state file
type pulumiState struct {
	Version    int               `json:"version"`
	Deployment pulumiDeployment  `json:"deployment"`
}

type pulumiDeployment struct {
	Resources []pulumiResource `json:"resources"`
}

type pulumiResource struct {
	URN          string   `json:"urn"`
	Custom       bool     `json:"custom"`
	Type         string   `json:"type"`
	Dependencies []string `json:"dependencies"`
}

// BuildGraphFromState creates a dependency graph from a Pulumi state file
func BuildGraphFromState(stateJSON []byte) (*Graph, error) {
	// Parse state file
	var state pulumiState
	if err := json.Unmarshal(stateJSON, &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}

	graph := &Graph{
		Nodes: make(map[string]*Node),
		Edges: make(map[string][]string),
	}

	// Build nodes from resources
	for _, res := range state.Deployment.Resources {
		// Skip non-custom resources (like Stack resources)
		if !res.Custom {
			continue
		}

		node := &Node{
			URN:          res.URN,
			Type:         res.Type,
			Dependencies: make([]string, len(res.Dependencies)),
			Dependents:   []string{},
		}

		// Copy dependencies
		copy(node.Dependencies, res.Dependencies)

		graph.Nodes[node.URN] = node
	}

	// Build edges (dependents)
	graph.buildEdges()

	return graph, nil
}

// buildEdges populates the Edges map and Dependents lists
func (g *Graph) buildEdges() {
	for urn, node := range g.Nodes {
		for _, depURN := range node.Dependencies {
			// depURN is a dependency of urn
			// So urn is a dependent of depURN
			g.Edges[depURN] = append(g.Edges[depURN], urn)

			// Also track in node
			if depNode := g.Nodes[depURN]; depNode != nil {
				depNode.Dependents = append(depNode.Dependents, urn)
			}
		}
	}
}

// TopologicalSort returns nodes in dependency order (leaves first)
// Uses Kahn's algorithm for topological sorting
func (g *Graph) TopologicalSort() ([]*Node, error) {
	// Calculate in-degree for each node
	inDegree := make(map[string]int)
	for urn, node := range g.Nodes {
		inDegree[urn] = len(node.Dependencies)
	}

	// Find all leaves (nodes with no dependencies)
	var queue []string
	for urn, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, urn)
		}
	}

	// Process nodes in topological order
	var sorted []*Node
	for len(queue) > 0 {
		// Dequeue
		urn := queue[0]
		queue = queue[1:]
		sorted = append(sorted, g.Nodes[urn])

		// Reduce in-degree of dependents
		for _, depURN := range g.Edges[urn] {
			inDegree[depURN]--
			if inDegree[depURN] == 0 {
				queue = append(queue, depURN)
			}
		}
	}

	// Check for cycle
	if len(sorted) != len(g.Nodes) {
		return nil, fmt.Errorf("cycle detected in dependency graph")
	}

	return sorted, nil
}
