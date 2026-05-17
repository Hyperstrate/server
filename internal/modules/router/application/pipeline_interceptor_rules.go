package application

import (
	"context"
	"log/slog"
	"regexp"
	"sync"

	"hyperstrate/server/internal/modules/router/domain"
)

// runSemanticClassifier embeds the prompt and finds the target whose utterance
// centroid is most similar. Returns nil when similarity < threshold.
func (p *featurePipeline) runSemanticClassifier(
	ctx context.Context,
	ic domain.RouterInterceptor,
	targets []domain.RouterTarget,
	fields map[string]string,
) (*domain.RouterTarget, error) {
	modelID, _ := ic.Config["model_id"].(string)
	threshold := float32(0)
	if v, ok := ic.Config["threshold"]; ok {
		if n := toFloat(v); n > 0 {
			threshold = float32(n)
		}
	}

	prompt := fields["prompt"]
	if prompt == "" {
		return nil, nil
	}

	promptEmb, err := p.embedder.Embed(ctx, modelID, prompt)
	if err != nil || len(promptEmb) == 0 {
		return nil, err
	}

	cfgTargets, _ := ic.Config["targets"].(map[string]any)
	var best *domain.RouterTarget
	var bestSim float32 = -2

	for i := range targets {
		t := &targets[i]
		utterances := extractUtterances(cfgTargets, t.ID)
		if len(utterances) == 0 {
			continue
		}
		utteranceEmbs := p.embCache.GetOrComputeAll(ctx, p.embedder, modelID, t.ID, utterances)
		if len(utteranceEmbs) == 0 {
			continue
		}
		var maxSim float32 = -2
		for _, uEmb := range utteranceEmbs {
			if s := cosineSimilarity(promptEmb, uEmb); s > maxSim {
				maxSim = s
			}
		}
		slog.Debug("semantic_classifier target scored", "targetID", t.ID, "utterances", utterances, "maxSim", maxSim)
		if maxSim > bestSim {
			bestSim = maxSim
			best = t
		}
	}

	if best == nil {
		slog.Debug("semantic_classifier no targets with utterances, falling through")
		return nil, nil
	}
	if bestSim < threshold {
		slog.Debug("semantic_classifier below threshold, falling through", "best", bestSim, "threshold", threshold)
		return nil, nil
	}
	slog.Debug("semantic_classifier matched", "best", bestSim, "threshold", threshold)
	return best, nil
}

func extractUtterances(cfgTargets map[string]any, targetID string) []string {
	if cfgTargets == nil {
		return nil
	}
	raw, _ := cfgTargets[targetID].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// runABTest assigns the request to a named variant and returns the matching
// target. Assignment is deterministic when partition_key is set and random otherwise.
func (p *featurePipeline) runABTest(
	ic domain.RouterInterceptor,
	targets []domain.RouterTarget,
	fields map[string]string,
) (*domain.RouterTarget, string) {
	type variant struct {
		name    string
		modelID string
		weight  int
	}

	rawVariants, _ := ic.Config["variants"].([]any)
	if len(rawVariants) == 0 {
		return nil, ""
	}

	variants := make([]variant, 0, len(rawVariants))
	totalWeight := 0
	for _, rv := range rawVariants {
		m, _ := rv.(map[string]any)
		if m == nil {
			continue
		}
		name, _ := m["name"].(string)
		modelID, _ := m["model_id"].(string)
		w := int(toFloat(m["weight"]))
		if w <= 0 {
			w = 1
		}
		if name == "" || modelID == "" {
			continue
		}
		variants = append(variants, variant{name: name, modelID: modelID, weight: w})
		totalWeight += w
	}
	if len(variants) == 0 || totalWeight == 0 {
		return nil, ""
	}

	var pick int
	partitionKey, _ := ic.Config["partition_key"].(string)
	if partitionKey != "" {
		if val := fields[partitionKey]; val != "" {
			pick = int(fnv32a(val+"|"+ic.ID) % uint32(totalWeight))
		} else {
			pick = randIntn(totalWeight)
		}
	} else {
		pick = randIntn(totalWeight)
	}

	cumulative := 0
	var selected *variant
	for i := range variants {
		cumulative += variants[i].weight
		if pick < cumulative {
			selected = &variants[i]
			break
		}
	}
	if selected == nil {
		selected = &variants[len(variants)-1]
	}

	for i := range targets {
		if targets[i].ModelID == selected.modelID {
			return &targets[i], selected.name
		}
	}
	return nil, ""
}

func fnv32a(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func applyContentFilter(fields map[string]string, cfg map[string]any) bool {
	prompt := fields["prompt"]
	if prompt == "" {
		return false
	}
	patterns, _ := cfg["blocked_patterns"].([]any)
	for _, p := range patterns {
		pat, _ := p.(string)
		if pat == "" {
			continue
		}
		re, err := cachedCompilePattern(pat)
		if err != nil {
			continue
		}
		if re.MatchString(prompt) {
			return true
		}
	}
	return false
}

var piiPatterns = map[string]struct {
	re   *regexp.Regexp
	mask string
}{
	// Email — word-boundary anchored to avoid matching inside URLs.
	"email": {
		regexp.MustCompile(`\b[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}\b`),
		"[EMAIL]",
	},
	// Phone — US (3-3-4) and international (+CC …) with common separators.
	"phone": {
		regexp.MustCompile(`(?:(?:\+|00)[1-9]\d{0,2}[\s.\-]?)?(?:\(?\d{2,4}\)?[\s.\-])?\d{3,4}[\s.\-]\d{3,4}(?:[\s.\-]\d{2,4})?(?:\s*(?:x|ext)\.?\s*\d{1,5})?`),
		"[PHONE]",
	},
	// SSN — dash or space separator (XXX-XX-XXXX / XXX XX XXXX).
	"ssn": {
		regexp.MustCompile(`\b\d{3}[- ]\d{2}[- ]\d{4}\b`),
		"[SSN]",
	},
	// Credit card — major schemes with optional dash/space grouping.
	// Visa (13/16d), MC (16d), Amex (15d 4-6-5), Discover (16d).
	"credit_card": {
		regexp.MustCompile(`\b(?:` +
			`4\d{3}[- ]?\d{4}[- ]?\d{4}[- ]?\d{4}|` + // Visa 16
			`4\d{3}[- ]?\d{4}[- ]?\d{5}|` + // Visa 13
			`5[1-5]\d{2}[- ]?\d{4}[- ]?\d{4}[- ]?\d{4}|` + // MasterCard
			`2(?:2[2-9]\d|[3-6]\d{2}|7[01]\d|720)\d[- ]?\d{4}[- ]?\d{4}[- ]?\d{4}|` + // MC 2-series
			`3[47]\d{2}[- ]?\d{6}[- ]?\d{5}|` + // Amex
			`6(?:011|5\d{2})[- ]?\d{4}[- ]?\d{4}[- ]?\d{4}` + // Discover
			`)\b`),
		"[CARD]",
	},
	// IPv4 — validates each octet 0–255 to avoid false positives on version strings etc.
	"ip_address": {
		regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`),
		"[IP]",
	},
	// IPv6 — full or compressed notation.
	"ipv6": {
		regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){2,7}[0-9a-f]{1,4}\b|` +
			`(?i)\b(?:[0-9a-f]{1,4}:)*::(?:[0-9a-f]{1,4}:)*[0-9a-f]{1,4}\b`),
		"[IPv6]",
	},
	// IBAN — two-letter country code, 2 check digits, up to 30 alphanumeric chars.
	"iban": {
		regexp.MustCompile(`\b[A-Z]{2}\d{2}[A-Z0-9]{10,30}\b`),
		"[IBAN]",
	},
	// Passport — one or two uppercase letters followed by 6–9 digits (covers most countries).
	"passport": {
		regexp.MustCompile(`\b[A-Z]{1,2}\d{7,9}\b`),
		"[PASSPORT]",
	},
	// Driver's licence — US formats: letter(s) + 5–9 digits or all-digit with state prefix.
	"drivers_license": {
		regexp.MustCompile(`\b[A-Z]\d{5,9}\b|\b\d{3}[- ]?\d{3}[- ]?\d{3}\b`),
		"[DL]",
	},
	// AWS access key IDs.
	"aws_key": {
		regexp.MustCompile(`\b(?:AKIA|ASIA|AROA|AIDA|ANPA|ANVA|AIPA)[A-Z0-9]{16}\b`),
		"[AWS_KEY]",
	},
	// Generic API / secret keys — high-entropy 32–64 char hex or base64-ish tokens
	// prefixed by common assignment patterns (key=, token=, secret=, api_key=, etc.).
	"api_key": {
		regexp.MustCompile(`(?i)(?:api[_\-]?key|secret[_\-]?key|access[_\-]?token|auth[_\-]?token|bearer)[=:\s"']+([A-Za-z0-9\-_.+/]{32,64})`),
		"[API_KEY]",
	},
}

func applyPIIDetector(fields map[string]string, cfg map[string]any) map[string]string {
	action, _ := cfg["action"].(string)
	if action == "" {
		action = "mask"
	}

	var active []string
	if raw, ok := cfg["entities"].([]any); ok && len(raw) > 0 {
		for _, e := range raw {
			if s, ok := e.(string); ok {
				active = append(active, s)
			}
		}
	} else {
		for k := range piiPatterns {
			active = append(active, k)
		}
	}

	out := make(map[string]string, len(fields))
	for k, v := range fields {
		out[k] = v
	}

	prompt := out["prompt"]
	for _, entity := range active {
		pat, ok := piiPatterns[entity]
		if !ok {
			continue
		}
		if action == "block" {
			if pat.re.MatchString(prompt) {
				out["__pii_blocked__"] = entity
				return out
			}
		} else {
			prompt = pat.re.ReplaceAllString(prompt, pat.mask)
		}
	}
	out["prompt"] = prompt
	return out
}

var jailbreakPatterns = map[string][]string{
	"moderate": {
		`ignore (all |previous |your )?instructions`,
		`disregard (all |your )?(previous |all )?instructions`,
		`bypass (your |all )?restrictions`,
		`override (your |all )?(previous )?instructions`,
		`do not (follow|obey|adhere to) (your |any )?(previous )?instructions`,
		`new (system )?instructions?:`,
		`\[(system|user|assistant)\]`,
	},
	"strict": {
		`ignore (all |previous |your )?instructions`,
		`disregard (all |your )?(previous |all )?instructions`,
		`bypass (your |all )?restrictions`,
		`override (your |all )?(previous )?instructions`,
		`do not (follow|obey|adhere to) (your |any )?(previous )?instructions`,
		`new (system )?instructions?:`,
		`\[(system|user|assistant)\]`,
		`pretend (you are|you have no|there are no)`,
		`act as (if|though) (you have no|there are no)`,
		`(roleplay|role-play|role play) as (a |an )?(human|person|unrestricted|jailbroken|evil|harmful)`,
		`(act|behave|respond) (as|like) (a |an )?(jailbroken|uncensored|unrestricted|unfiltered) (ai|model|assistant)`,
		`from now on (you will |you are )?(no longer |not )?(an ai|bound by|restricted)`,
		`you are (now )?(dan|jailbreak|unrestricted|without restrictions)`,
		`(jailbreak|developer mode|dan mode|do anything now)`,
		`(enable|unlock|activate) (unrestricted|developer|jailbreak|god) mode`,
		`forget (you are|your|all) (an ai|a language model|instructions)`,
		`(reset|clear|wipe) (your )?(instructions|guidelines|training|safety)`,
		`your (true|real|hidden) (self|nature|personality|identity)`,
		`(simulate|imagine|hypothetically) (that )?(you|an ai) (have no|lack|don't have) (restrictions|guidelines|safety)`,
		`what would (a|an) (human|unrestricted|evil|dangerous|jailbroken) (ai|model|person|assistant) (say|do|respond|answer)`,
		`<\|im_start\|>|<\|endoftext\|>|<\|system\|>`,
		`your (safety|content|ethical) (policy|filter|guidelines|restrictions?) (is|are|has been) (disabled|off|removed|bypassed)`,
	},
}

var jailbreakRegexps map[string][]*regexp.Regexp
var compiledPatternCache sync.Map // string → *regexp.Regexp

func init() {
	jailbreakRegexps = make(map[string][]*regexp.Regexp, len(jailbreakPatterns))
	for level, pats := range jailbreakPatterns {
		compiled := make([]*regexp.Regexp, 0, len(pats))
		for _, p := range pats {
			compiled = append(compiled, regexp.MustCompile(`(?i)`+p))
		}
		jailbreakRegexps[level] = compiled
	}
}

func cachedCompilePattern(pat string) (*regexp.Regexp, error) {
	if v, ok := compiledPatternCache.Load(pat); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(`(?i)` + pat)
	if err != nil {
		return nil, err
	}
	compiledPatternCache.Store(pat, re)
	return re, nil
}

func applyPromptGuard(fields map[string]string, cfg map[string]any) bool {
	prompt := fields["prompt"]
	if prompt == "" {
		return false
	}
	policy, _ := cfg["policy"].(string)
	if policy != "moderate" {
		policy = "strict"
	}
	for _, re := range jailbreakRegexps[policy] {
		if re.MatchString(prompt) {
			return true
		}
	}
	if raw, ok := cfg["custom_patterns"].([]any); ok {
		for _, p := range raw {
			pat, _ := p.(string)
			if pat == "" {
				continue
			}
			re, err := cachedCompilePattern(pat)
			if err != nil {
				continue
			}
			if re.MatchString(prompt) {
				return true
			}
		}
	}
	return false
}
