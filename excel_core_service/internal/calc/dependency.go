// calc/dependency.go：跨表公式/Lookup/关联依赖图与保存期环检测。
package calc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// FieldRef identifies one field across table boundaries.
type FieldRef struct {
	TableID string
	FieldID string
}

func (reference FieldRef) String() string {
	return reference.TableID + "." + reference.FieldID
}

// DependencyCycleError is returned by save validation when the combined
// formula, lookup and relation graph contains a cycle.
type DependencyCycleError struct {
	Cycle []FieldRef
}

func (err *DependencyCycleError) Error() string {
	if err == nil || len(err.Cycle) == 0 {
		return "循环引用"
	}
	names := make([]string, 0, len(err.Cycle))
	for _, reference := range err.Cycle {
		names = append(names, reference.String())
	}
	return "循环引用: " + strings.Join(names, " -> ")
}

type dependencyGraph struct {
	nodes map[FieldRef]FieldDefinition
	edges map[FieldRef]map[FieldRef]struct{}
}

// ValidateSchema is the save-time seam for 08/structure. It loads the complete
// field graph and rejects direct or relation/lookup-mediated cycles.
func (service *Service) ValidateSchema(ctx context.Context) error {
	if service == nil || service.schema == nil {
		return errors.New("calc schema provider is required")
	}
	fields, err := service.schema.Fields(ctx)
	if err != nil {
		return fmt.Errorf("load calc schema: %w", err)
	}
	return ValidateDependencyGraph(fields)
}

// ValidateDependencyGraph validates a complete field snapshot without a
// provider, allowing structure writes to run the check in their transaction.
func ValidateDependencyGraph(fields []FieldDefinition) error {
	graph, err := buildDependencyGraph(fields)
	if err != nil {
		return err
	}
	return graph.Validate()
}

// Validate returns the first deterministic cycle in the graph.
func (graph *dependencyGraph) Validate() error {
	cycles := graph.FindCycles()
	if len(cycles) == 0 {
		return nil
	}
	return &DependencyCycleError{Cycle: append([]FieldRef(nil), cycles[0]...)}
}

// FindCycles returns unique cycles, excluding the repeated start node.
func (graph *dependencyGraph) FindCycles() [][]FieldRef {
	if graph == nil {
		return nil
	}
	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[FieldRef]int, len(graph.nodes))
	stack := make([]FieldRef, 0, len(graph.nodes))
	stackIndex := make(map[FieldRef]int, len(graph.nodes))
	seen := make(map[string]struct{})
	cycles := make([][]FieldRef, 0)

	var visit func(FieldRef)
	visit = func(node FieldRef) {
		state[node] = visiting
		stackIndex[node] = len(stack)
		stack = append(stack, node)
		for _, neighbor := range graph.sortedNeighbors(node) {
			switch state[neighbor] {
			case unvisited:
				visit(neighbor)
			case visiting:
				start := stackIndex[neighbor]
				cycle := append([]FieldRef(nil), stack[start:]...)
				cycle = canonicalCycle(cycle)
				key := cycleKey(cycle)
				if _, exists := seen[key]; !exists {
					seen[key] = struct{}{}
					cycles = append(cycles, cycle)
				}
			}
		}
		stack = stack[:len(stack)-1]
		delete(stackIndex, node)
		state[node] = visited
	}

	for _, node := range graph.sortedNodes() {
		if state[node] == unvisited {
			visit(node)
		}
	}
	sort.Slice(cycles, func(left, right int) bool {
		return cycleKey(cycles[left]) < cycleKey(cycles[right])
	})
	return cycles
}

func buildDependencyGraph(fields []FieldDefinition) (*dependencyGraph, error) {
	graph := &dependencyGraph{
		nodes: make(map[FieldRef]FieldDefinition, len(fields)),
		edges: make(map[FieldRef]map[FieldRef]struct{}, len(fields)),
	}
	byTableID := make(map[string]map[string]FieldRef)
	byTableName := make(map[string]map[string]FieldRef)
	for _, field := range fields {
		if field.TableID == "" || field.ID == "" {
			return nil, errors.New("calc schema field requires table_id and field_id")
		}
		reference := FieldRef{TableID: field.TableID, FieldID: field.ID}
		if _, exists := graph.nodes[reference]; exists {
			return nil, fmt.Errorf("duplicate calc field %s", reference)
		}
		graph.nodes[reference] = field
		graph.edges[reference] = make(map[FieldRef]struct{})
		if byTableID[field.TableID] == nil {
			byTableID[field.TableID] = make(map[string]FieldRef)
			byTableName[field.TableID] = make(map[string]FieldRef)
		}
		byTableID[field.TableID][field.ID] = reference
		if field.Name != "" {
			if _, exists := byTableName[field.TableID][field.Name]; exists {
				return nil, fmt.Errorf("duplicate calc field name %q in table %s", field.Name, field.TableID)
			}
			byTableName[field.TableID][field.Name] = reference
		}
	}

	findInTable := func(tableID, idOrName string) (FieldRef, bool) {
		if reference, ok := byTableID[tableID][idOrName]; ok {
			return reference, true
		}
		reference, ok := byTableName[tableID][idOrName]
		return reference, ok
	}

	for _, field := range fields {
		source := FieldRef{TableID: field.TableID, FieldID: field.ID}
		if field.Type == "FIELD_TYPE_FORMULA" || field.Formula != "" || len(field.FormulaRefs) > 0 {
			references := append([]string(nil), field.FormulaRefs...)
			if field.Formula != "" {
				parsed, err := parseFormula(field.Formula)
				if err != nil {
					return nil, fmt.Errorf("formula %s: %w", source, err)
				}
				references = append(references, expressionReferences(parsed)...)
			}
			for _, name := range uniqueStrings(references) {
				target, ok := findInTable(field.TableID, name)
				if !ok {
					return nil, fmt.Errorf("公式 %s 引用不存在字段 %q", source, name)
				}
				graph.addEdge(source, target)
			}
		}
		if field.Relation != nil {
			target := FieldRef{TableID: field.Relation.TableID, FieldID: field.Relation.FieldID}
			if _, ok := graph.nodes[target]; !ok {
				return nil, fmt.Errorf("关联 %s 指向不存在字段 %s", source, target)
			}
			graph.addEdge(source, target)
		}
		if field.Lookup != nil {
			if err := graph.addLookupEdges(field, source, findInTable); err != nil {
				return nil, err
			}
		}
	}
	return graph, nil
}

func (graph *dependencyGraph) addLookupEdges(
	field FieldDefinition,
	source FieldRef,
	findInTable func(string, string) (FieldRef, bool),
) error {
	currentTable := field.TableID
	chain := field.Lookup.RelationChain
	if len(chain) == 0 {
		relation, ok := findInTable(currentTable, field.Lookup.RelationFieldID)
		if !ok {
			return fmt.Errorf("Lookup %s 引用不存在关联字段 %q", source, field.Lookup.RelationFieldID)
		}
		relationField := graph.nodes[relation]
		if relationField.Relation == nil {
			return fmt.Errorf("Lookup %s 引用的字段 %s 不是关联字段", source, relation)
		}
		targetID := field.Lookup.TargetFieldID
		if targetID == "" {
			targetID = relationField.Relation.FieldID
		}
		target := FieldRef{TableID: relationField.Relation.TableID, FieldID: targetID}
		if _, ok := graph.nodes[target]; !ok {
			return fmt.Errorf("Lookup %s 指向不存在字段 %s", source, target)
		}
		graph.addEdge(source, relation)
		graph.addEdge(source, target)
		return nil
	}

	for _, step := range chain {
		relation, ok := findInTable(currentTable, step.FieldID)
		if !ok {
			return fmt.Errorf("Lookup %s 关联链引用不存在字段 %q", source, step.FieldID)
		}
		relationField := graph.nodes[relation]
		if relationField.Relation == nil {
			return fmt.Errorf("Lookup %s 关联链字段 %s 不是关联字段", source, relation)
		}
		if step.TableID != "" && step.TableID != relationField.Relation.TableID {
			return fmt.Errorf("Lookup %s 关联链目标表不一致: %s", source, step.TableID)
		}
		graph.addEdge(source, relation)
		currentTable = relationField.Relation.TableID
	}
	target, ok := findInTable(currentTable, field.Lookup.TargetFieldID)
	if !ok {
		return fmt.Errorf("Lookup %s 关联链末端字段不存在 %q", source, field.Lookup.TargetFieldID)
	}
	graph.addEdge(source, target)
	return nil
}

func (graph *dependencyGraph) addEdge(source, target FieldRef) {
	if graph == nil {
		return
	}
	if graph.edges[source] == nil {
		graph.edges[source] = make(map[FieldRef]struct{})
	}
	graph.edges[source][target] = struct{}{}
}

func (graph *dependencyGraph) sortedNodes() []FieldRef {
	nodes := make([]FieldRef, 0, len(graph.nodes))
	for node := range graph.nodes {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(left, right int) bool {
		return nodes[left].String() < nodes[right].String()
	})
	return nodes
}

func (graph *dependencyGraph) sortedNeighbors(node FieldRef) []FieldRef {
	neighbors := make([]FieldRef, 0, len(graph.edges[node]))
	for neighbor := range graph.edges[node] {
		neighbors = append(neighbors, neighbor)
	}
	sort.Slice(neighbors, func(left, right int) bool {
		return neighbors[left].String() < neighbors[right].String()
	})
	return neighbors
}

func expressionReferences(expression expression) []string {
	switch typed := expression.(type) {
	case referenceExpression:
		return []string{typed.name}
	case unaryExpression:
		return expressionReferences(typed.operand)
	case binaryExpression:
		return uniqueStrings(append(
			expressionReferences(typed.left),
			expressionReferences(typed.right)...,
		))
	case callExpression:
		references := make([]string, 0)
		for _, argument := range typed.arguments {
			references = append(references, expressionReferences(argument)...)
		}
		return uniqueStrings(references)
	default:
		return nil
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func canonicalCycle(cycle []FieldRef) []FieldRef {
	if len(cycle) < 2 {
		return append([]FieldRef(nil), cycle...)
	}
	best := 0
	for index := 1; index < len(cycle); index++ {
		if cycle[index].String() < cycle[best].String() {
			best = index
		}
	}
	result := make([]FieldRef, 0, len(cycle))
	result = append(result, cycle[best:]...)
	result = append(result, cycle[:best]...)
	return result
}

func cycleKey(cycle []FieldRef) string {
	values := make([]string, 0, len(cycle))
	for _, reference := range cycle {
		values = append(values, reference.String())
	}
	return strings.Join(values, "->")
}
