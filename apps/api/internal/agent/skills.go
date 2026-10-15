package agent

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
)

//go:embed skills/ddshub-analysis/SKILL.md skills/ddshub-analysis/references/*.md skills/web-research/SKILL.md
var skillFiles embed.FS

const (
	SkillName        = "ddshub-analysis"
	SkillDescription = "分析 DDSHUB 充值、使用和客户指标；按需读取数据字典，通过只读 MCP 查询并提供依据。来源由服务标识为模拟或线上只读数据。"
)

func SkillVersion() string {
	h := sha256.New()
	for _, name := range []string{"SKILL.md", "references/data-dictionary.md", "references/query-examples.md"} {
		content, _ := skillFiles.ReadFile("skills/" + SkillName + "/" + name)
		h.Write([]byte(name))
		h.Write(content)
	}

	web, _ := skillFiles.ReadFile("skills/web-research/SKILL.md")
	h.Write(web)
	return hex.EncodeToString(h.Sum(nil))
}

func ReadSkill(name, path string) (string, error) {
	if name == WebSkillName && path == "SKILL.md" {
		content, err := skillFiles.ReadFile("skills/web-research/SKILL.md")
		return string(content), err
	}
	if name != SkillName || (path != "SKILL.md" && path != "references/data-dictionary.md" && path != "references/query-examples.md") {
		return "", errors.New("Skill 或资料不在允许范围内")
	}

	content, err := skillFiles.ReadFile("skills/" + name + "/" + path)
	return string(content), err
}
