// Package router 根据关键词将消息路由到对应的项目配置
package router

import (
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"strings"
)

// Matcher 关键词匹配器
type Matcher struct{}

func NewMatcher() *Matcher {
	return &Matcher{}
}

// Match 根据消息内容和意图识别结果，返回最匹配的项目路由
// 匹配规则（优先级从高到低）：
//  1. 意图中的 suspected_project 与路由 name 完全匹配
//  2. 消息命中路由关键词数量最多（多关键词命中）
//  3. 按路由的 priority 字段降序
func (m *Matcher) Match(message string, intent *model.IntentResult) (*model.ProjectRoute, error) {
	routes, err := store.ListRoutes(true) // 只查启用的
	if err != nil {
		return nil, err
	}
	if len(routes) == 0 {
		return nil, nil
	}

	msgLower := strings.ToLower(message)

	type scored struct {
		route *model.ProjectRoute
		score int
	}

	var candidates []scored
	for _, r := range routes {
		score := 0

		// 规则 1：suspected_project 匹配
		if intent != nil && intent.SuspectedProject != "" {
			if strings.EqualFold(r.Name, intent.SuspectedProject) {
				score += 1000
			}
		}

		// 规则 2：关键词命中（每命中一个 +10，+路由优先级）
		for _, kw := range r.Keywords {
			if kw == "" {
				continue
			}
			if strings.Contains(msgLower, strings.ToLower(kw)) {
				score += 10
			}
		}

		// 意图关键词也参与匹配
		if intent != nil {
			for _, kw := range intent.MatchedKeywords {
				for _, rk := range r.Keywords {
					if strings.EqualFold(kw, rk) {
						score += 5
					}
				}
			}
		}

		// 规则 3：加上路由自身 priority
		score += r.Priority

		if score > 0 {
			candidates = append(candidates, scored{r, score})
		}
	}

	if len(candidates) == 0 {
		// 没有命中时返回优先级最高的路由（兜底）
		if len(routes) > 0 {
			return routes[0], nil
		}
		return nil, nil
	}

	// 按 score 降序排，取第一个
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		}
	}
	return best.route, nil
}
