package indicatorcalc

import "testing"

func TestCoreFeatureDefinitionsAreValid(t *testing.T) {
	definitions := CoreFeatureDefinitions()
	if err := ValidateFeatureDefinitions(definitions); err != nil {
		t.Fatalf("ValidateFeatureDefinitions() error = %v", err)
	}
	if len(definitions) == 0 {
		t.Fatal("CoreFeatureDefinitions() is empty")
	}
}

func TestValidateFeatureDefinitionsRejectsDuplicates(t *testing.T) {
	err := ValidateFeatureDefinitions([]FeatureDefinition{
		{Name: "rsi14", Kind: FeatureKindIndicator, ValueType: ValueTypeFloat},
		{Name: "rsi14", Kind: FeatureKindIndicator, ValueType: ValueTypeFloat},
	})
	if err == nil {
		t.Fatal("ValidateFeatureDefinitions() error = nil, want duplicate error")
	}
}

func TestMetadataParameterHashIsOrderIndependent(t *testing.T) {
	first := Metadata(Options{SMAPeriods: []int{25, 7}, EMAPeriods: []int{99, 7}, WMAPeriods: []int{25, 7}})
	second := Metadata(Options{SMAPeriods: []int{7, 25}, EMAPeriods: []int{7, 99}, WMAPeriods: []int{7, 25}})
	if first.ParameterHash != second.ParameterHash {
		t.Fatalf("parameter hash differs: %q != %q", first.ParameterHash, second.ParameterHash)
	}
	if first.SchemaVersion == "" || first.CalculatorVersion == "" {
		t.Fatalf("metadata versions are empty: %#v", first)
	}
}
