package patch

import (
	"fmt"

	"forge/internal/tools"
)

func ApplyWrite(root, path, content string) error {
	return tools.WriteFile(root, path, content)
}

func ApplyReplace(root, path, find, replace string) error {
	if find == "" {
		return fmt.Errorf("replace action missing find text for %s", path)
	}
	return tools.ReplaceInFile(root, path, find, replace)
}
