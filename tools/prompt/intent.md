请分析以下用户消息，判断意图类型。

用户消息：
{{.Message}}
{{if .ProjectList}}
当前已配置的项目列表（suspected_project 必须从中选择，无匹配则留空）：
{{.ProjectList}}{{end}}
请严格按照如下 JSON 格式输出，不得有任何额外内容：
{
  "intent": "<issue_troubleshooting|requirement_writing|ignore|need_more_context|risky_action>",
  "confidence": <0.0-1.0>,
  "matched_keywords": ["<keyword1>", "<keyword2>"],
  "suspected_project": "<从上方项目列表中选择最匹配的项目名，无匹配则空字符串>",
  "need_repo_access": <true|false>,
  "need_doc_access": <true|false>,
  "need_db_query": <true|false>,
  "risk_level": "<low|medium|high|critical>",
  "summary": "<一句话摘要>"
}
