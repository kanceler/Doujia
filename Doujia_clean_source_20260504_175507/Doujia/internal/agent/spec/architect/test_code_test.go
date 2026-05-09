package architect

import "testing"

func TestTestCodeSpecAcceptsFullDeliveryInputBags(t *testing.T) {
	t.Parallel()

	spec := TestCodeSpec()
	got := map[string]bool{}
	for _, bag := range spec.InputBags {
		got[bag.Name] = bag.Required
	}

	for _, name := range []string{"merged_code", "global_test_data", "container_context"} {
		if !got[name] {
			t.Fatalf("TestCodeSpec().InputBags[%q] required = %v, want true; got %+v", name, got[name], got)
		}
	}
	if got["global_test_code_input"] {
		t.Fatalf("TestCodeSpec().InputBags[global_test_code_input] required = true, want compatibility-only optional; got %+v", got)
	}
}
