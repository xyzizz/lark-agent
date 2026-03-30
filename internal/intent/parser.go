package intent

import (
	"encoding/json"
	"feishu-agent/internal/model"
	"fmt"
	"regexp"
	"strings"
)

var validIntents = map[string]bool{
	model.IntentIssueTroubleshooting: true,
	model.IntentRequirementWriting:   true,
	model.IntentIgnore:               true,
	model.IntentNeedMoreContext:      true,
	model.IntentRiskyAction:          true,
}

var validRiskLevels = map[string]bool{
	"low": true, "medium": true, "high": true, "critical": true,
}

// ParseIntentJSON 解析 LLM 返回的 JSON 意图结果
// 支持：纯 JSON、Markdown 代码块包裹的 JSON
func ParseIntentJSON(raw string) (*model.IntentResult, error) {
	raw = strings.TrimSpace(raw)

	// 尝试从 Markdown 代码块中提取 JSON
	jsonStr := extractJSON(raw)
	if jsonStr == "" {
		jsonStr = raw
	}

	var result model.IntentResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w (raw: %s)", err, truncate(raw, 200))
	}

	// 校验并修正字段
	if !validIntents[result.Intent] {
		result.Intent = model.IntentNeedMoreContext
	}
	if result.Confidence < 0 || result.Confidence > 1 {
		result.Confidence = 0.5
	}
	if !validRiskLevels[result.RiskLevel] {
		result.RiskLevel = "low"
	}
	if result.MatchedKeywords == nil {
		result.MatchedKeywords = []string{}
	}
	if len(result.Summary) > 200 {
		result.Summary = result.Summary[:200]
	}

	return &result, nil
}

// extractJSON 从文本中提取 JSON 块
// 支持 ```json ... ``` 和 ``` ... ```
var reCodeBlock = regexp.MustCompile("(?s)```(?:json)?\\s*({.+?})\\s*```")
var reRawJSON = regexp.MustCompile("(?s)({.+})")

func extractJSON(text string) string {
	// 优先匹配代码块
	if m := reCodeBlock.FindStringSubmatch(text); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	// 再尝试直接提取大括号
	if m := reRawJSON.FindStringSubmatch(text); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
