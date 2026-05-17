package application

import (
	"math"
	"sync"
	"sync/atomic"

	"hyperstrate/server/internal/modules/router/domain"
)

type ucb1Arm struct {
	rewards atomic.Value // float64
	trials  int64        // atomic
}

type ucb1State struct {
	mu   sync.RWMutex
	arms map[string]*ucb1Arm
}

func (s *ucb1State) getOrCreate(name string) *ucb1Arm {
	s.mu.RLock()
	arm, ok := s.arms[name]
	s.mu.RUnlock()
	if ok {
		return arm
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if arm, ok = s.arms[name]; ok {
		return arm
	}
	arm = &ucb1Arm{}
	arm.rewards.Store(float64(0))
	s.arms[name] = arm
	return arm
}

func (s *ucb1State) selectBest() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := int64(0)
	for _, arm := range s.arms {
		total += atomic.LoadInt64(&arm.trials)
	}

	bestName := ""
	bestScore := -1.0
	for name, arm := range s.arms {
		t := atomic.LoadInt64(&arm.trials)
		r := arm.rewards.Load().(float64)
		var score float64
		if t == 0 {
			score = math.MaxFloat64 // unexplored arm — always try first
		} else {
			mean := r / float64(t)
			exploration := math.Sqrt(2 * math.Log(float64(total+1)) / float64(t))
			score = mean + exploration
		}
		if score > bestScore {
			bestScore = score
			bestName = name
		}
	}
	return bestName
}

func (p *featurePipeline) getOrCreateUCB1(interceptorID string) *ucb1State {
	v, _ := p.ucbState.LoadOrStore(interceptorID, &ucb1State{arms: make(map[string]*ucb1Arm)})
	return v.(*ucb1State)
}

func (p *featurePipeline) updateUCB1(interceptorID, variant string, reward float64) {
	state := p.getOrCreateUCB1(interceptorID)
	arm := state.getOrCreate(variant)
	atomic.AddInt64(&arm.trials, 1)
	for {
		old := arm.rewards.Load().(float64)
		if arm.rewards.CompareAndSwap(old, old+reward) {
			break
		}
	}
}

// runABTestOrUCB1 extends runABTest to support mode: "ucb1".
// When UCB1 mode is active, arm selection uses the UCB1 bandit algorithm
// and the selected arm/interceptor IDs are recorded in options for post-inference update.
func (p *featurePipeline) runABTestOrUCB1(
	ic domain.RouterInterceptor,
	targets []domain.RouterTarget,
	fields map[string]string,
	options map[string]any,
) (*domain.RouterTarget, string) {
	mode, _ := ic.Config["mode"].(string)
	if mode != "ucb1" {
		return p.runABTest(ic, targets, fields)
	}

	// UCB1 mode: select arm using exploration/exploitation balance
	type variant struct {
		name    string
		modelID string
	}
	rawVariants, _ := ic.Config["variants"].([]any)
	variants := make([]variant, 0, len(rawVariants))
	for _, rv := range rawVariants {
		m, _ := rv.(map[string]any)
		if m == nil {
			continue
		}
		name, _ := m["name"].(string)
		modelID, _ := m["model_id"].(string)
		if name != "" && modelID != "" {
			variants = append(variants, variant{name, modelID})
		}
	}
	if len(variants) == 0 {
		return nil, ""
	}

	state := p.getOrCreateUCB1(ic.ID)
	// Ensure all arms are known to the state
	for _, v := range variants {
		state.getOrCreate(v.name)
	}

	selectedName := state.selectBest()
	for _, v := range variants {
		if v.name == selectedName {
			// Record interceptor+variant in options so the post-Phase-9 UCB1 update
			// can identify which arm to credit. Write directly into the caller's map
			// (options is a reference type in Go).
			if options == nil {
				options = make(map[string]any)
			}
			options["_ucb1_interceptor_id"] = ic.ID
			options["_ucb1_variant"] = selectedName
			// Find matching target
			for i := range targets {
				if targets[i].ModelID == v.modelID {
					return &targets[i], selectedName
				}
			}
		}
	}
	return nil, ""
}
