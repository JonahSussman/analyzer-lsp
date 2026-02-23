package provider

import (
	"fmt"
	"testing"

	"go.lsp.dev/uri"
)

// deduplicateDependencies was using len(deduped) (the outer map) instead of
// len(deduped[uri]) (the inner slice) when storing dep indices. This meant
// the stored index was almost always 0, so upgrading an indirect dep to
// direct would modify the wrong entry.
func TestDeduplicateDependencies_UpgradeIndirectToDirect(t *testing.T) {
	testURI := uri.URI("file:///test/pom.xml")
	deps := map[uri.URI][]*Dep{
		testURI: {
			{Name: "libA", Version: "1.0", Indirect: false},
			{Name: "libB", Version: "2.0", Indirect: true},
			{Name: "libC", Version: "3.0", Indirect: false},
			{Name: "libB", Version: "2.0", Indirect: false}, // should upgrade libB at index 1
		},
	}

	result := deduplicateDependencies(deps)
	resultDeps := result[testURI]

	if len(resultDeps) != 3 {
		t.Fatalf("expected 3 unique deps, got %d", len(resultDeps))
	}
	if resultDeps[0].Name != "libA" || resultDeps[0].Indirect != false {
		t.Errorf("libA: expected direct, got Indirect=%v", resultDeps[0].Indirect)
	}
	if resultDeps[1].Name != "libB" || resultDeps[1].Indirect != false {
		t.Errorf("libB: expected Indirect=false (upgraded to direct), got Indirect=%v", resultDeps[1].Indirect)
	}
	if resultDeps[2].Name != "libC" || resultDeps[2].Indirect != false {
		t.Errorf("libC: expected direct, got Indirect=%v", resultDeps[2].Indirect)
	}
}

// Inlined copy of the old buggy deduplication to show the failure.
func TestDeduplicateDependencies_OldBuggyBehavior(t *testing.T) {
	buggyDedup := func(dependencies map[uri.URI][]*Dep) map[uri.URI][]*Dep {
		intPtr := func(i int) *int { return &i }
		deduped := map[uri.URI][]*Dep{}
		for uri, deps := range dependencies {
			deduped[uri] = []*Dep{}
			depSeen := map[string]*int{}
			for _, dep := range deps {
				id := dep.Name + dep.Version + dep.ResolvedIdentifier
				if depSeen[id+"direct"] != nil {
					continue
				} else if depSeen[id+"indirect"] != nil {
					if !dep.Indirect {
						deduped[uri][*depSeen[id+"indirect"]].Indirect = false
						depSeen[id+"direct"] = depSeen[id+"indirect"]
					} else {
						continue
					}
				} else {
					deduped[uri] = append(deduped[uri], dep)
					if dep.Indirect {
						depSeen[id+"indirect"] = intPtr(len(deduped) - 1) // bug: outer map len
					} else {
						depSeen[id+"direct"] = intPtr(len(deduped) - 1)
					}
				}
			}
		}
		return deduped
	}

	testURI := uri.URI("file:///test/pom.xml")
	deps := map[uri.URI][]*Dep{
		testURI: {
			{Name: "libA", Version: "1.0", Indirect: false},
			{Name: "libB", Version: "2.0", Indirect: true},
			{Name: "libC", Version: "3.0", Indirect: false},
			{Name: "libB", Version: "2.0", Indirect: false},
		},
	}

	result := buggyDedup(deps)
	resultDeps := result[testURI]

	// libB stays indirect because the stored index pointed at 0 (libA) instead of 1
	if resultDeps[1].Indirect != true {
		t.Error("expected buggy code to leave libB as Indirect=true")
	}
}

// validateUpdateInternalProviderConfig had a variable shadowing bug: the
// recursive call used := which created a local "new" that shadowed the outer
// "new". Then `new[s] = new` assigned the local map to itself and the outer
// map was never updated, silently dropping nested configs.
func TestValidateUpdateInternalProviderConfig_NestedMap(t *testing.T) {
	input := map[interface{}]interface{}{
		"name": "my-provider",
		"database": map[interface{}]interface{}{
			"host": "localhost",
			"port": 5432,
		},
	}

	result, err := validateUpdateInternalProviderConfig(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["name"] != "my-provider" {
		t.Errorf("expected name='my-provider', got %v", result["name"])
	}
	dbRaw, ok := result["database"]
	if !ok {
		t.Fatal("'database' key missing from result, nested map was lost")
	}
	db, ok := dbRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'database' to be map[string]interface{}, got %T", dbRaw)
	}
	if db["host"] != "localhost" {
		t.Errorf("expected database.host='localhost', got %v", db["host"])
	}
	if db["port"] != 5432 {
		t.Errorf("expected database.port=5432, got %v", db["port"])
	}
}

// Inlined copy of the old buggy validation to show the failure.
func TestValidateUpdateInternalProviderConfig_OldBuggyBehavior(t *testing.T) {
	buggyValidate := func(old map[interface{}]interface{}) (map[string]interface{}, error) {
		new := map[string]interface{}{}
		for k, v := range old {
			s, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("key is not a string")
			}
			if o, ok := v.(map[interface{}]interface{}); ok {
				new, err := buggyValidateHelper(o) // shadows outer "new"
				if err != nil {
					return nil, err
				}
				new[s] = new // assigns local map to itself, outer map never updated
				continue
			}
			new[s] = v
		}
		return new, nil
	}

	input := map[interface{}]interface{}{
		"name": "my-provider",
		"database": map[interface{}]interface{}{
			"host": "localhost",
			"port": 5432,
		},
	}

	result, err := buggyValidate(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result["database"]; ok {
		t.Error("expected buggy code to drop 'database' key")
	}
}

func buggyValidateHelper(old map[interface{}]interface{}) (map[string]interface{}, error) {
	result := map[string]interface{}{}
	for k, v := range old {
		s, ok := k.(string)
		if !ok {
			return nil, fmt.Errorf("key is not a string")
		}
		result[s] = v
	}
	return result, nil
}
