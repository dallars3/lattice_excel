package calc

import (
	"context"
	"errors"
	"testing"
)

type staticSchemaProvider struct {
	fields []FieldDefinition
	err    error
}

func (provider staticSchemaProvider) Fields(context.Context) ([]FieldDefinition, error) {
	return provider.fields, provider.err
}

func TestValidateSchemaRejectsIndirectRelationLookupCycle(t *testing.T) {
	fields := relationLookupCycleFixture()
	graph, err := buildDependencyGraph(fields)
	if err != nil {
		t.Fatalf("buildDependencyGraph() error = %v", err)
	}
	if cycles := graph.FindCycles(); len(cycles) == 0 {
		t.Fatal("FindCycles() = none, want relation-lookup cycle")
	}

	service := NewService(nil, staticSchemaProvider{fields: fields}, nil, nil, nil)
	err = service.ValidateSchema(context.Background())
	var cycleError *DependencyCycleError
	if !errors.As(err, &cycleError) {
		t.Fatalf("ValidateSchema() error = %v, want DependencyCycleError", err)
	}
	if len(cycleError.Cycle) < 3 {
		t.Fatalf("cycle = %+v, want at least three fields", cycleError.Cycle)
	}
}

func TestValidateSchemaAcceptsAcyclicRelationLookupGraph(t *testing.T) {
	fields := relationLookupCycleFixture()
	for index := range fields {
		if fields[index].ID == "b_formula" {
			fields[index].Formula = "1+1"
			fields[index].FormulaRefs = nil
		}
	}
	service := NewService(nil, staticSchemaProvider{fields: fields}, nil, nil, nil)
	if err := service.ValidateSchema(context.Background()); err != nil {
		t.Fatalf("ValidateSchema() error = %v, want nil", err)
	}
}

func relationLookupCycleFixture() []FieldDefinition {
	return []FieldDefinition{
		{
			ID: "a_rel", Name: "ARel", TableID: "table_a", Type: "FIELD_TYPE_RELATION",
			Relation: &RelationTarget{TableID: "table_b", FieldID: "b_formula"},
		},
		{
			ID: "a_lookup", Name: "ALookup", TableID: "table_a", Type: "FIELD_TYPE_LOOKUP",
			Lookup: &LookupSource{RelationFieldID: "a_rel", TargetFieldID: "b_formula"},
		},
		{
			ID: "a_formula", Name: "AFormula", TableID: "table_a", Type: "FIELD_TYPE_FORMULA",
			Formula: "{ALookup}+1", FormulaRefs: []string{"ALookup"},
		},
		{
			ID: "b_rel", Name: "BRel", TableID: "table_b", Type: "FIELD_TYPE_RELATION",
			Relation: &RelationTarget{TableID: "table_a", FieldID: "a_formula"},
		},
		{
			ID: "b_lookup", Name: "BLookup", TableID: "table_b", Type: "FIELD_TYPE_LOOKUP",
			Lookup: &LookupSource{RelationFieldID: "b_rel", TargetFieldID: "a_formula"},
		},
		{
			ID: "b_formula", Name: "BFormula", TableID: "table_b", Type: "FIELD_TYPE_FORMULA",
			Formula: "{BLookup}+1", FormulaRefs: []string{"BLookup"},
		},
	}
}
