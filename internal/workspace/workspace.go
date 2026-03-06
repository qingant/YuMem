package workspace

import (
	"os"
	"path/filepath"
	"yumem/internal/config"
)

var globalConfig *config.Config

func Initialize(workspaceDir string) error {
	globalConfig = config.GetDefault(workspaceDir)

	// Create necessary directories
	dirs := []string{
		globalConfig.L0Dir,
		filepath.Join(globalConfig.L0Dir, "current"),
		globalConfig.L1Dir,
		filepath.Join(globalConfig.L1Dir, "nodes"),
		globalConfig.L2Dir,
		filepath.Join(globalConfig.L2Dir, "content"),
		filepath.Dir(globalConfig.LogFile),
		filepath.Join(globalConfig.WorkspaceDir, "_yumem", "versions"),
		filepath.Join(globalConfig.WorkspaceDir, "_yumem", "prompts", "import"),
		filepath.Join(globalConfig.WorkspaceDir, "_yumem", "prompts", "context_assembly"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	// Write default prompt templates (won't overwrite existing)
	if err := writeDefaultPromptTemplates(); err != nil {
		return err
	}

	return nil
}

func GetConfig() *config.Config {
	return globalConfig
}

func EnsureInitialized() error {
	if globalConfig == nil {
		return Initialize("")
	}
	return nil
}

func writeDefaultPromptTemplates() error {
	promptDir := filepath.Join(globalConfig.WorkspaceDir, "_yumem", "prompts", "import")
	path := filepath.Join(promptDir, "analyze_content.md")

	// Don't overwrite if already exists
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	return os.WriteFile(path, []byte(analyzeContentPrompt), 0644)
}

const analyzeContentPrompt = `你是一个记忆管理系统的分析引擎。你的目标是：让这个系统能够像用户了解自己一样，去了解与他对话的人。

分析以下内容，提取两类信息：

## L0：用户画像（每次对话都携带的核心信息）

L0 存储的是用户的核心身份信息——性格、背景、能力、偏好等。这些信息在每次 AI 对话时都会被携带，让 AI 能够提供个性化的回应。

请提取任何有助于理解用户的特征，自行组织为语义化的分类。注意时间线索——如果能推断出某个特征的时间段，请标注 valid_from 和 valid_until。

例如（仅供参考，不限于此）：
- background: 教育经历、职业经历、生活经历
- skills: 技术能力、专业领域、语言能力
- personality: 性格特征、沟通风格、行为偏好
- interests: 兴趣爱好、关注领域
- philosophy: 价值观、信念、原则
- ...任何你认为有助于理解用户的维度

当前 L0 状态（已有的用户画像）：
{{.l0_current}}

## L1：语义索引（按需检索的主题知识）

L1 是一个树状的知识索引。每个节点代表一个主题，包含摘要和关键词，用于按需检索。
如果内容值得索引为一个知识主题，请生成一个 L1 节点。
如果内容太碎片化、无实质信息、或者纯粹是个人特征（已由 L0 覆盖），l1_node 可以为 null。

当前 L1 结构（已有的知识索引）：
{{range $path, $summary := .l1_structure}}
- {{$path}}: {{$summary}}
{{end}}
{{if not .l1_structure}}（暂无）{{end}}

## 原始内容

来源：{{.source}}
L2 ID：{{.l2_id}}
---
{{.content}}
---

请返回 JSON（不要包含 markdown 代码块标记）：
{
  "l0_updates": {
    "category_name": {
      "key": "value"
    }
  },
  "l0_agenda": [
    {
      "item": "议程描述",
      "priority": "high|medium|low"
    }
  ],
  "l1_node": {
    "path": "category/subcategory/topic",
    "title": "节点标题",
    "summary": "1-2 句摘要（注意体现时间上下文）",
    "keywords": ["keyword1", "keyword2", "keyword3"]
  },
  "reasoning": "简要说明你的判断依据"
}

重要规则：
- l0_updates、l0_agenda、l1_node 都可以为空对象/空数组/null
- 只提取你有信心的信息，不要猜测
- 如果内容中包含时间线索，请在 value 中体现（如 "软件工程师 (2022至今)"）
- 对于 l1_node 的 path，优先使用已有的路径，必要时才创建新路径
- 返回纯 JSON，不要包含 ` + "`" + `json 等 markdown 标记
`
