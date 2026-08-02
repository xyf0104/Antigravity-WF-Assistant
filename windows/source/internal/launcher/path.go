package launcher

import "strings"

func windowsPathWithinRoot(imagePath, installRoot string) bool {
	normalize := func(value string) string {
		value = strings.Trim(strings.TrimSpace(value), `"`)
		value = strings.ReplaceAll(value, "/", `\`)
		return strings.TrimRight(value, `\`)
	}
	image := normalize(imagePath)
	root := normalize(installRoot)
	if image == "" || root == "" {
		return false
	}
	return strings.EqualFold(image, root) || strings.HasPrefix(strings.ToLower(image), strings.ToLower(root)+`\`)
}
