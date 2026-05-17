package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func applyTokenOptimization(fields map[string]string, cfg map[string]any) map[string]string {
	maxChars := int(toFloat(cfg["max_chars"]))
	if maxChars <= 0 {
		return fields
	}
	out := make(map[string]string, len(fields))
	for k, v := range fields {
		if k == "prompt" && len(v) > maxChars {
			v = v[:maxChars]
		}
		out[k] = v
	}
	return out
}

func applyContextTrimming(fields map[string]string, cfg map[string]any) map[string]string {
	maxChars := int(toFloat(cfg["max_chars"]))
	if maxChars <= 0 {
		return fields
	}
	out := make(map[string]string, len(fields))
	for k, v := range fields {
		if k == "prompt" && len(v) > maxChars {
			v = v[len(v)-maxChars:]
		}
		out[k] = v
	}
	return out
}

type tokenCostOptimizationResult struct {
	BeforeChars int
	AfterChars  int
	Fields      []string
	MaxTokens   int
	ModelAction string
	ModelID     string
	ModelError  string
}

func (r tokenCostOptimizationResult) detail() string {
	parts := []string{fmt.Sprintf("%d to %d chars", r.BeforeChars, r.AfterChars)}
	if len(r.Fields) > 0 {
		parts = append(parts, "fields: "+strings.Join(r.Fields, ", "))
	}
	if r.MaxTokens > 0 {
		parts = append(parts, fmt.Sprintf("max_tokens: %d", r.MaxTokens))
	}
	if r.ModelAction != "" {
		modelDetail := "model: " + r.ModelAction
		if r.ModelID != "" {
			modelDetail += " (" + r.ModelID + ")"
		}
		if r.ModelError != "" {
			modelDetail += ": " + truncateStepDetail(r.ModelError, 180)
		}
		parts = append(parts, modelDetail)
	}
	return strings.Join(parts, " · ")
}

func applyTokenCostOptimization(fields map[string]string, options map[string]any, cfg map[string]any) (map[string]string, map[string]any, tokenCostOptimizationResult) {
	selectedFields := extractStringSlice(cfg["fields"])
	if len(selectedFields) == 0 {
		selectedFields = []string{"prompt", "systemPrompt", "_history"}
	}

	out := make(map[string]string, len(fields))
	for k, v := range fields {
		out[k] = v
	}

	result := tokenCostOptimizationResult{
		BeforeChars: totalFieldChars(fields),
	}

	for _, name := range selectedFields {
		value, ok := out[name]
		if !ok || value == "" {
			continue
		}
		rewritten := rewriteTokenCostField(name, value, cfg)
		if rewritten != value {
			out[name] = rewritten
			result.Fields = append(result.Fields, name)
		}
	}

	if maxPromptChars := int(toFloat(cfg["max_prompt_chars"])); maxPromptChars > 0 {
		if value := out["prompt"]; len(value) > maxPromptChars {
			out["prompt"] = trimToCharBudget(value, maxPromptChars, tokenCostTrimStrategy(cfg, "prompt"))
			if !containsString(result.Fields, "prompt") {
				result.Fields = append(result.Fields, "prompt")
			}
		}
	}

	if maxTokens := int(toFloat(cfg["output_max_tokens"])); maxTokens > 0 {
		options = copyOptions(options)
		if current, ok := options["max_tokens"]; !ok || int(toFloat(current)) == 0 || int(toFloat(current)) > maxTokens {
			options["max_tokens"] = maxTokens
			result.MaxTokens = maxTokens
		}
	}

	result.AfterChars = totalFieldChars(out)
	return out, options, result
}

func applyModelTokenCostOptimization(
	ctx context.Context,
	fields map[string]string,
	cfg map[string]any,
	inferencer ModelInferencer,
	result *tokenCostOptimizationResult,
) map[string]string {
	modelID, _ := cfg["rewrite_model_id"].(string)
	if modelID == "" || inferencer == nil {
		return fields
	}
	result.ModelID = modelID

	fieldName, _ := cfg["rewrite_field"].(string)
	if fieldName == "" {
		fieldName = "prompt"
	}
	value := fields[fieldName]
	if value == "" {
		result.ModelAction = "skipped_empty"
		return fields
	}
	minChars := int(toFloat(cfg["rewrite_min_chars"]))
	if minChars <= 0 {
		minChars = 4000
	}
	if len(value) < minChars {
		result.ModelAction = "skipped_short"
		return fields
	}

	targetChars := int(toFloat(cfg["rewrite_target_chars"]))
	if ratio := toFloat(cfg["rewrite_target_ratio"]); targetChars <= 0 && ratio > 0 && ratio < 1 {
		targetChars = int(float64(len(value)) * ratio)
	}
	if targetChars <= 0 {
		targetChars = minChars
	}
	if targetChars >= len(value) {
		result.ModelAction = "skipped_budget"
		return fields
	}

	instruction, _ := cfg["rewrite_instruction"].(string)
	if instruction == "" {
		instruction = "Rewrite the user payload to reduce token usage while preserving every concrete requirement, constraint, identifier, number, and fact. Remove repetition and verbosity. Do not answer the task. Return only the rewritten payload."
	}

	rewriteOptions := map[string]any{
		"temperature": 0.0,
		"max_tokens":  charBudgetToTokenBudget(targetChars),
	}
	rewritePrompt := fmt.Sprintf("%s\n\nTarget maximum characters: %d\n\nPayload:\n%s", instruction, targetChars, value)
	rewrite, err := inferencer.InferModel(ctx, modelID, map[string]string{"prompt": rewritePrompt}, rewriteOptions)
	if err != nil {
		result.ModelAction = "error"
		result.ModelError = err.Error()
		return fields
	}
	candidate := strings.TrimSpace(rewrite.Content)
	if candidate == "" {
		result.ModelAction = "skipped_empty_result"
		return fields
	}
	if len(candidate) >= len(value) {
		result.ModelAction = "skipped_not_smaller"
		return fields
	}
	if minSavings := toFloat(cfg["rewrite_min_savings_ratio"]); minSavings > 0 {
		savings := float64(len(value)-len(candidate)) / float64(len(value))
		if savings < minSavings {
			result.ModelAction = "skipped_low_savings"
			return fields
		}
	}

	out := make(map[string]string, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	out[fieldName] = candidate
	result.ModelAction = "rewrote"
	if !containsString(result.Fields, fieldName) {
		result.Fields = append(result.Fields, fieldName)
	}
	result.AfterChars = totalFieldChars(out)
	return out
}

func charBudgetToTokenBudget(chars int) int {
	tokens := chars / 4
	if tokens < 64 {
		return 64
	}
	return tokens
}

func rewriteTokenCostField(name, value string, cfg map[string]any) string {
	out := value
	if boolCfg(cfg, "trim_space", true) {
		out = strings.TrimSpace(out)
	}
	if boolCfg(cfg, "minify_json", true) {
		if compacted, ok := minifyJSONString(out); ok {
			out = compacted
		}
	}
	if boolCfg(cfg, "collapse_blank_lines", true) {
		out = collapseBlankLines(out)
	}
	if boolCfg(cfg, "compact_whitespace", false) {
		out = strings.Join(strings.Fields(out), " ")
	}
	if boolCfg(cfg, "dedupe_lines", false) {
		out = dedupeLines(out)
	}
	if maxChars := tokenCostMaxChars(cfg, name); maxChars > 0 && len(out) > maxChars {
		out = trimToCharBudget(out, maxChars, tokenCostTrimStrategy(cfg, name))
	}
	return out
}

type promptOptimizerResult struct {
	BeforeChars int
	AfterChars  int
	Fields      []string
}

func (r promptOptimizerResult) detail() string {
	parts := []string{fmt.Sprintf("%d to %d chars", r.BeforeChars, r.AfterChars)}
	if len(r.Fields) > 0 {
		parts = append(parts, "fields: "+strings.Join(r.Fields, ", "))
	}
	return strings.Join(parts, " · ")
}

func applyPromptOptimizer(fields map[string]string, cfg map[string]any) (map[string]string, promptOptimizerResult) {
	selectedFields := extractStringSlice(cfg["fields"])
	if len(selectedFields) == 0 {
		selectedFields = []string{"prompt"}
	}

	out := make(map[string]string, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	result := promptOptimizerResult{BeforeChars: totalFieldChars(fields)}

	for _, name := range selectedFields {
		value, ok := out[name]
		if !ok || value == "" {
			continue
		}
		if _, ok := minifyJSONString(strings.TrimSpace(value)); ok {
			continue
		}
		rewritten := applyPromptOptimizerChain(value, cfg)
		if rewritten != value {
			out[name] = rewritten
			result.Fields = append(result.Fields, name)
		}
	}
	result.AfterChars = totalFieldChars(out)
	return out, result
}

func applyPromptOptimizerChain(value string, cfg map[string]any) string {
	optimizers := extractStringSlice(cfg["optimizers"])
	out := value
	for _, optimizer := range optimizers {
		switch optimizer {
		case "punctuation":
			out = applyProtected(out, cfg, removePromptPunctuation)
		case "stopwords":
			out = applyProtected(out, cfg, removeStopWords)
		case "compact_whitespace":
			out = applyProtected(out, cfg, func(s string) string { return strings.Join(strings.Fields(s), " ") })
		case "dedupe_lines":
			out = applyProtected(out, cfg, dedupeLines)
		case "lowercase":
			out = applyProtected(out, cfg, strings.ToLower)
		}
	}
	return out
}

type protectedTag struct {
	Start string
	End   string
}

func applyProtected(value string, cfg map[string]any, fn func(string) string) string {
	tags := protectedTags(cfg)
	if len(tags) == 0 {
		return fn(value)
	}

	var b strings.Builder
	rest := value
	for len(rest) > 0 {
		tag, start := nextProtectedStart(rest, tags)
		if start < 0 {
			b.WriteString(fn(rest))
			break
		}
		b.WriteString(fn(rest[:start]))
		afterStart := start + len(tag.Start)
		end := strings.Index(rest[afterStart:], tag.End)
		if end < 0 {
			b.WriteString(rest[start:])
			break
		}
		protectedEnd := afterStart + end + len(tag.End)
		b.WriteString(rest[start:protectedEnd])
		rest = rest[protectedEnd:]
	}
	return b.String()
}

func protectedTags(cfg map[string]any) []protectedTag {
	tags := []protectedTag{
		{Start: "<protected>", End: "</protected>"},
		{Start: "[[KEEP]]", End: "[[/KEEP]]"},
	}
	raw, _ := cfg["protected_tags"].([]any)
	for _, item := range raw {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		start, _ := m["start"].(string)
		end, _ := m["end"].(string)
		if start != "" && end != "" {
			tags = append(tags, protectedTag{Start: start, End: end})
		}
	}
	return tags
}

func nextProtectedStart(value string, tags []protectedTag) (protectedTag, int) {
	bestIdx := -1
	var best protectedTag
	for _, tag := range tags {
		idx := strings.Index(value, tag.Start)
		if idx >= 0 && (bestIdx < 0 || idx < bestIdx) {
			bestIdx = idx
			best = tag
		}
	}
	return best, bestIdx
}

func removePromptPunctuation(value string) string {
	var b strings.Builder
	lastSpace := false
	for _, r := range value {
		if isPromptPunctuation(r) {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		b.WriteRune(r)
		lastSpace = r == ' ' || r == '\n' || r == '\t'
	}
	return strings.TrimSpace(strings.Join(strings.Fields(b.String()), " "))
}

func isPromptPunctuation(r rune) bool {
	switch r {
	case '.', ',', ';', ':', '!', '?', '"', '\'', '(', ')', '[', ']', '{', '}':
		return true
	default:
		return false
	}
}

var promptOptimizerStopWords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "of": {}, "to": {}, "is": {}, "are": {}, "was": {}, "were": {},
	"be": {}, "been": {}, "being": {}, "in": {}, "on": {}, "at": {}, "by": {}, "for": {}, "with": {},
	"as": {}, "that": {}, "this": {}, "these": {}, "those": {}, "please": {}, "kindly": {},
}

func removeStopWords(value string) string {
	words := strings.Fields(value)
	out := make([]string, 0, len(words))
	for _, word := range words {
		key := strings.ToLower(strings.Trim(word, ".,;:!?\"'()[]{}"))
		if _, ok := promptOptimizerStopWords[key]; ok {
			continue
		}
		out = append(out, word)
	}
	return strings.Join(out, " ")
}

func minifyJSONString(value string) (string, bool) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(value)); err != nil {
		return "", false
	}
	return buf.String(), true
}

func collapseBlankLines(value string) string {
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		isBlank := strings.TrimSpace(line) == ""
		if isBlank && blank {
			continue
		}
		out = append(out, strings.TrimRight(line, " \t"))
		blank = isBlank
	}
	return strings.Join(out, "\n")
}

func dedupeLines(value string) string {
	lines := strings.Split(value, "\n")
	seen := map[string]struct{}{}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		key := strings.TrimSpace(line)
		if key != "" {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func tokenCostMaxChars(cfg map[string]any, field string) int {
	if raw, ok := cfg["max_chars"].(map[string]any); ok {
		return int(toFloat(raw[field]))
	}
	return 0
}

func tokenCostTrimStrategy(cfg map[string]any, field string) string {
	if raw, ok := cfg["trim_strategy"].(map[string]any); ok {
		if strategy, _ := raw[field].(string); strategy != "" {
			return strategy
		}
	}
	if strategy, _ := cfg["trim_strategy"].(string); strategy != "" {
		return strategy
	}
	return "tail"
}

func trimToCharBudget(value string, maxChars int, strategy string) string {
	if maxChars <= 0 || len(value) <= maxChars {
		return value
	}
	switch strategy {
	case "head", "start":
		return value[:maxChars]
	case "middle":
		if maxChars <= 3 {
			return value[:maxChars]
		}
		head := (maxChars - 3) / 2
		tail := maxChars - 3 - head
		return value[:head] + "..." + value[len(value)-tail:]
	default:
		return value[len(value)-maxChars:]
	}
}

func totalFieldChars(fields map[string]string) int {
	total := 0
	for _, value := range fields {
		total += len(value)
	}
	return total
}

func boolCfg(cfg map[string]any, key string, def bool) bool {
	if v, ok := cfg[key].(bool); ok {
		return v
	}
	return def
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

// compressHistory evicts lowest-relevance turns from the JSON-encoded _history
// field to keep total character count under maxChars.
func compressHistory(historyJSON, currentPrompt string, maxChars, keepRecent int) string {
	var msgs []map[string]any
	if err := json.Unmarshal([]byte(historyJSON), &msgs); err != nil || len(msgs) == 0 {
		return ""
	}
	if len(msgs) <= keepRecent || len(historyJSON) <= maxChars {
		return ""
	}
	keep := len(msgs) - keepRecent
	candidates := msgs[:keep]

	promptWords := wordSet(currentPrompt)
	type scored struct {
		idx   int
		score float64
	}
	scores := make([]scored, len(candidates))
	for i, msg := range candidates {
		content, _ := msg["content"].(string)
		scores[i] = scored{i, wordOverlap(content, promptWords)}
	}
	for i := 1; i < len(scores); i++ {
		for j := i; j > 0 && scores[j].score < scores[j-1].score; j-- {
			scores[j], scores[j-1] = scores[j-1], scores[j]
		}
	}

	evict := make(map[int]bool)
	current := len(historyJSON)
	for _, s := range scores {
		if current <= maxChars {
			break
		}
		content, _ := candidates[s.idx]["content"].(string)
		current -= len(content) + 30
		evict[s.idx] = true
	}
	if len(evict) == 0 {
		return ""
	}

	surviving := make([]map[string]any, 0, len(msgs))
	for i, msg := range msgs[:keep] {
		if !evict[i] {
			surviving = append(surviving, msg)
		}
	}
	surviving = append(surviving, msgs[keep:]...)
	b, _ := json.Marshal(surviving)
	return string(b)
}

func wordSet(s string) map[string]bool {
	words := strings.Fields(strings.ToLower(s))
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[w] = true
	}
	return set
}

func wordOverlap(content string, promptWords map[string]bool) float64 {
	words := strings.Fields(strings.ToLower(content))
	if len(words) == 0 {
		return 0
	}
	matches := 0
	for _, w := range words {
		if promptWords[w] {
			matches++
		}
	}
	return float64(matches) / float64(len(words))
}

func costAwareTarget(cfg map[string]any, totalChars int) string {
	thresholds, _ := cfg["thresholds"].([]any)
	for _, raw := range thresholds {
		m, _ := raw.(map[string]any)
		if m == nil {
			continue
		}
		maxChars := int(toFloat(m["max_chars"]))
		targetID, _ := m["target_id"].(string)
		if maxChars > 0 && totalChars <= maxChars && targetID != "" {
			return targetID
		}
	}
	defaultID, _ := cfg["default_target_id"].(string)
	return defaultID
}
