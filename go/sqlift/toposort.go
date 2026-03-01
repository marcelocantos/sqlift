// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sqlift

import "sort"

// extractSQLReferences tokenizes sql into identifier-like words (alphanumeric
// + underscore) and returns those that appear in knownNames but are not
// ownName.
func extractSQLReferences(sql, ownName string, knownNames map[string]bool) map[string]bool {
	refs := map[string]bool{}
	word := []byte{}
	for i := 0; i <= len(sql); i++ {
		var c byte
		if i < len(sql) {
			c = sql[i]
		}
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			word = append(word, c)
		} else {
			if len(word) > 0 {
				w := string(word)
				if w != ownName && knownNames[w] {
					refs[w] = true
				}
				word = word[:0]
			}
		}
	}
	return refs
}

// topoSort performs a topological sort of nodes using Kahn's algorithm.
// deps maps each node to the set of nodes it depends on. If reverse is true,
// the result is reversed. Returns a DiffError if a cycle is detected.
func topoSort(nodes []string, deps map[string]map[string]bool, reverse bool) ([]string, error) {
	inDegree := make(map[string]int, len(nodes))
	dependents := make(map[string][]string)

	for _, n := range nodes {
		inDegree[n] = 0
	}

	for _, n := range nodes {
		for dep := range deps[n] {
			if _, ok := inDegree[dep]; ok {
				inDegree[n]++
				dependents[dep] = append(dependents[dep], n)
			}
		}
	}

	queue := []string{}
	for _, n := range nodes {
		if inDegree[n] == 0 {
			queue = append(queue, n)
		}
	}
	sort.Strings(queue)

	result := make([]string, 0, len(nodes))
	for front := 0; front < len(queue); front++ {
		n := queue[front]
		result = append(result, n)
		if deps := dependents[n]; len(deps) > 0 {
			newlyFree := []string{}
			for _, dep := range deps {
				inDegree[dep]--
				if inDegree[dep] == 0 {
					newlyFree = append(newlyFree, dep)
				}
			}
			sort.Strings(newlyFree)
			queue = append(queue, newlyFree...)
		}
	}

	if len(result) != len(nodes) {
		return nil, &DiffError{"Circular dependency detected among views/triggers"}
	}

	if reverse {
		for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
			result[i], result[j] = result[j], result[i]
		}
	}

	return result, nil
}
