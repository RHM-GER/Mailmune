package classifier

import "github.com/RHM-GER/Mailmune/internal/domain"

const CandidateThreshold = 0.60

type Action string

const (
	ActionIgnore Action = "ignore"
	ActionQueue  Action = "queue"
	ActionMove   Action = "move"
)

func Decide(mode domain.SafetyMode, result domain.Classification) Action {
	if result.Score < CandidateThreshold {
		return ActionIgnore
	}
	if mode == domain.SafetyConfirmAll {
		return ActionQueue
	}
	if result.StrongTrustSignal || result.IndependentGroups < 2 {
		return ActionQueue
	}
	threshold := 0.98
	if mode == domain.SafetyAggressive {
		threshold = 0.80
	}
	if result.Score >= threshold {
		return ActionMove
	}
	return ActionQueue
}
