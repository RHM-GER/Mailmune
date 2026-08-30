package classifier

import (
	"testing"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

func TestModelAloneCannotMove(t *testing.T) {
	result := domain.Classification{Score: 0.999, IndependentGroups: 1, ModelUsed: "test", ModelValidated: true}
	if got := Decide(domain.SafetyAggressive, result); got != ActionQueue {
		t.Fatalf("LLM-only result moved message: %s", got)
	}
}

func TestStrongTrustNeverAutoMoves(t *testing.T) {
	result := domain.Classification{Score: 1, IndependentGroups: 4, StrongTrustSignal: true}
	if got := Decide(domain.SafetySafe, result); got != ActionQueue {
		t.Fatalf("trusted correspondent moved: %s", got)
	}
}

func TestSafeRequiresStrictThreshold(t *testing.T) {
	result := domain.Classification{Score: 0.97, IndependentGroups: 2}
	if got := Decide(domain.SafetySafe, result); got != ActionQueue {
		t.Fatalf("safe mode threshold bypassed: %s", got)
	}
	result.Score = 0.98
	if got := Decide(domain.SafetySafe, result); got != ActionMove {
		t.Fatalf("safe decision not moved: %s", got)
	}
}
