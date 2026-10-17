package assistant

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

// Skill 资源随二进制分发：启动助手前写入工作区，正文变化才覆盖。
//
//go:embed skills
var skillFS embed.FS

//go:embed system.md
var systemPrompt []byte

// skillNames 是随包分发的全部 Skill。
var skillNames = []string{"yovoice-score", "yovoice-lyrics"}

// writeAssets 把系统说明与 Skill 写入数据目录，内容没有变化时不重写文件。
func writeAssets(root string) error {
	base := filepath.Join(root, "assistant")
	for _, dir := range []string{filepath.Join(base, "workspace", ".agents", "skills"), filepath.Join(base, "config")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	if err := writeFileIfChanged(systemPromptPath(root), systemPrompt); err != nil {
		return err
	}
	for _, name := range skillNames {
		body, err := skillFS.ReadFile("skills/" + name + "/SKILL.md")
		if err != nil {
			return fmt.Errorf("embedded skill %s: %w", name, err)
		}
		if err = writeFileIfChanged(filepath.Join(base, "workspace", ".agents", "skills", name, "SKILL.md"), body); err != nil {
			return err
		}
	}
	return nil
}

func writeFileIfChanged(path string, body []byte) error {
	if current, err := os.ReadFile(path); err == nil && string(current) == string(body) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0600)
}
