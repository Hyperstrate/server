package application

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"hyperstrate/server/internal/modules/router/domain"
)

// runPromptShield evaluates the prompt against LLM-based policies.
// Returns (activated bool, blocked bool, detail string).
func (p *featurePipeline) runPromptShield(
	ctx context.Context,
	ic domain.RouterInterceptor,
	fields map[string]string,
	inferencer ModelInferencer,
) (activated bool, blocked bool, detail string) {
	shieldModelID, _ := ic.Config["shield_model_id"].(string)
	if shieldModelID == "" {
		return true, true, "prompt_shield misconfigured: shield_model_id is required"
	}
	policies, _ := ic.Config["policies"].([]any)
	prompt := fields["prompt"]
	if prompt == "" {
		return false, false, ""
	}

	for _, rawPolicy := range policies {
		pol, _ := rawPolicy.(map[string]any)
		if pol == nil {
			continue
		}
		policyPrompt, _ := pol["prompt"].(string)
		action, _ := pol["action"].(string)
		name, _ := pol["name"].(string)
		if policyPrompt == "" {
			continue
		}

		evalPrompt := fmt.Sprintf("%s\n\nUser message:\n%s\n\nAnswer only YES or NO.", policyPrompt, prompt)
		r, err := inferencer.InferModel(ctx, shieldModelID, map[string]string{"prompt": evalPrompt}, nil)
		if err != nil {
			continue
		}
		answer := strings.TrimSpace(strings.ToUpper(r.Content))
		if !strings.HasPrefix(answer, "YES") {
			continue
		}
		// Policy matched
		detail = fmt.Sprintf("policy '%s' matched", name)
		switch action {
		case "block":
			return true, true, detail
		default: // "flag"
			return true, false, detail
		}
	}
	return false, false, ""
}

// teamBudgetCounter tracks per-team spending within a pipeline instance.
type teamBudgetCounter struct {
	mu               sync.Mutex
	periodKey        string
	estimatedCostUSD float64
	requests         int64
}

// checkTeamBudget checks whether teamID has exceeded its configured budget.
// Returns (overflowTarget, blocked, detail).
// - If the team is over budget and an overflow target is configured, returns (target, false, detail).
// - If the team is over budget with no overflow, returns (nil, true, detail).
// - If the team is within budget, returns (nil, false, "").
func (p *featurePipeline) checkTeamBudget(
	ic domain.RouterInterceptor,
	teamID string,
	enabled []domain.RouterTarget,
) (*domain.RouterTarget, bool, string) {
	budgets, _ := ic.Config["budgets"].(map[string]any)
	if budgets == nil {
		return nil, false, ""
	}
	rawTeamBudget, ok := budgets[teamID]
	if !ok {
		return nil, false, "" // team not in config, allow
	}
	teamCfg, _ := rawTeamBudget.(map[string]any)
	if teamCfg == nil {
		return nil, false, ""
	}

	maxCostUSD := toFloat(teamCfg["max_cost_usd"])
	period, _ := teamCfg["period"].(string)
	overflowTargetID, _ := teamCfg["overflow_target_id"].(string)

	key := ic.ID + ":" + teamID + ":" + budgetPeriodStart(period).Format("2006-01")
	actual, _ := p.teamBudgets.LoadOrStore(key, &teamBudgetCounter{periodKey: budgetPeriodStart(period).Format("2006-01")})
	bc := actual.(*teamBudgetCounter)

	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.periodKey != budgetPeriodStart(period).Format("2006-01") {
		bc.periodKey, bc.estimatedCostUSD, bc.requests = budgetPeriodStart(period).Format("2006-01"), 0, 0
	}

	if maxCostUSD > 0 && bc.estimatedCostUSD >= maxCostUSD {
		detail := fmt.Sprintf("team %s budget exhausted ($%.2f/$%.2f)", teamID, bc.estimatedCostUSD, maxCostUSD)
		if overflowTargetID != "" {
			for i := range enabled {
				if enabled[i].ModelID == overflowTargetID {
					return &enabled[i], false, detail + " to overflow target"
				}
			}
		}
		return nil, true, detail
	}
	return nil, false, ""
}
