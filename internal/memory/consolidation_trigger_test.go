package memory

import "testing"

func TestShouldTriggerConsolidation(t *testing.T) {
	if !ShouldTriggerConsolidation("Thanks, that is all", 0) {
		t.Fatal("completion phrase did not trigger consolidation")
	}
	if ShouldTriggerConsolidation("keep working", ConsolidationTurnCeiling-1) {
		t.Fatal("early turn ceiling triggered consolidation")
	}
	if !ShouldTriggerConsolidation("keep working", ConsolidationTurnCeiling) {
		t.Fatal("turn ceiling did not trigger consolidation")
	}
}
