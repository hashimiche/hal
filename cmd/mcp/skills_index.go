package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type skillDoc struct {
	Path        string   `json:"path"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Commands    []string `json:"commands,omitempty"`
}

type skillIndex struct {
	Skills              []skillDoc          `json:"skills"`
	Commands            []string            `json:"commands"`
	DeprecatedCommands  map[string]string   `json:"deprecated_commands"`
	CommandsByActionKey map[string][]string `json:"commands_by_action_key"`
}

var (
	skillIndexOnce sync.Once
	skillIndexData *skillIndex
	skillIndexErr  error
)

func getSkillIndex() (*skillIndex, error) {
	skillIndexOnce.Do(func() {
		skillIndexData, skillIndexErr = loadSkillIndex()
	})
	return skillIndexData, skillIndexErr
}

func loadSkillIndex() (*skillIndex, error) {
	skillsDir, err := resolveSkillsDir()
	if err != nil {
		return nil, err
	}

	idx := &skillIndex{
		Skills:              []skillDoc{},
		Commands:            []string{},
		DeprecatedCommands:  map[string]string{},
		CommandsByActionKey: map[string][]string{},
	}

	seenCommands := map[string]bool{}

	walkErr := filepath.WalkDir(skillsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}

		contentBytes, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		content := string(contentBytes)
		name, description := parseSkillFrontmatter(content)
		commands := extractHALCommands(content)

		for _, cmd := range commands {
			if !seenCommands[cmd] {
				seenCommands[cmd] = true
				idx.Commands = append(idx.Commands, cmd)
			}
			if actionKey, ok := inferActionKeyFromCommand(cmd); ok {
				idx.CommandsByActionKey[actionKey] = appendUnique(idx.CommandsByActionKey[actionKey], cmd)
			}
		}

		idx.Skills = append(idx.Skills, skillDoc{
			Path:        path,
			Name:        name,
			Description: description,
			Commands:    commands,
		})

		idx.DeprecatedCommands = mergeDeprecated(idx.DeprecatedCommands, parseDeprecatedCommands(content))
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	sort.Strings(idx.Commands)
	sort.Slice(idx.Skills, func(i, j int) bool { return idx.Skills[i].Path < idx.Skills[j].Path })
	return idx, nil
}

func resolveSkillsDir() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("HAL_SKILLS_DIR")); custom != "" {
		if info, err := os.Stat(custom); err == nil && info.IsDir() {
			return custom, nil
		}
	}

	if wd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(wd, ".github", "copilot", "skills")
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("skills directory not found (expected .github/copilot/skills); set HAL_SKILLS_DIR to override")
}

func parseSkillFrontmatter(content string) (string, string) {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return "", ""
	}
	name := ""
	desc := ""
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			break
		}
		if strings.HasPrefix(line, "name:") {
			name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "name:")), "\"")
		}
		if strings.HasPrefix(line, "description:") {
			desc = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "description:")), "\"")
		}
	}
	return name, desc
}

func extractHALCommands(content string) []string {
	re := regexp.MustCompile(`(?m)^\s*(?:[-*]\s+)?hal\s+[^\n]+$`)
	matches := re.FindAllString(content, -1)
	seen := map[string]bool{}
	commands := make([]string, 0, len(matches))
	for _, m := range matches {
		cmd := strings.TrimSpace(m)
		cmd = strings.TrimPrefix(cmd, "- ")
		cmd = strings.TrimPrefix(cmd, "* ")
		cmd = strings.Trim(cmd, "`")
		cmd = strings.TrimSuffix(cmd, ".")
		cmd = strings.TrimSpace(cmd)
		if !strings.HasPrefix(cmd, "hal ") {
			continue
		}
		cmd = normalizeDisplayCommand(cmd)
		if cmd == "" || seen[cmd] {
			continue
		}
		seen[cmd] = true
		commands = append(commands, cmd)
	}
	sort.Strings(commands)
	return commands
}

func normalizeDisplayCommand(cmd string) string {
	parts := strings.Fields(strings.TrimSpace(cmd))
	if len(parts) == 0 {
		return ""
	}
	if parts[0] != "hal" {
		return ""
	}
	if len(parts) > 1 && parts[1] == "tf" {
		parts[1] = "terraform"
	}
	if len(parts) > 1 && parts[1] == "observability" {
		parts[1] = "obs"
	}
	for i := range parts {
		if parts[i] == "-e" {
			parts[i] = "--enable"
		}
		if parts[i] == "-d" {
			parts[i] = "--disable"
		}
	}
	return strings.Join(parts, " ")
}

func inferActionKeyFromCommand(cmd string) (string, bool) {
	parts := strings.Fields(cmd)
	if len(parts) < 2 || parts[0] != "hal" {
		return "", false
	}
	root := parts[1]
	sub := ""
	for i := 2; i < len(parts); i++ {
		if strings.HasPrefix(parts[i], "-") {
			continue
		}
		sub = parts[i]
		break
	}
	if sub == "" {
		if root == "status" || root == "capacity" || root == "catalog" || root == "destroy" || root == "version" {
			return root, true
		}
		return root + "_status", true
	}
	key := root + "_" + sub
	if strings.Contains(cmd, " --enable") {
		key += "_enable"
	}
	if strings.Contains(cmd, " --disable") {
		key += "_disable"
	}
	return key, true
}

func parseDeprecatedCommands(content string) map[string]string {
	deprecated := map[string]string{}
	lower := strings.ToLower(content)
	if !strings.Contains(lower, "deprecated") && !strings.Contains(lower, "removed") {
		return deprecated
	}

	commandRe := regexp.MustCompile("`(hal\\s+[^`]+)`")
	all := commandRe.FindAllStringSubmatch(content, -1)
	commands := make([]string, 0, len(all))
	seen := map[string]bool{}
	for _, m := range all {
		if len(m) > 1 {
			cmd := normalizeDisplayCommand(strings.TrimSpace(m[1]))
			if cmd != "" && !seen[cmd] {
				seen[cmd] = true
				commands = append(commands, cmd)
			}
		}
	}
	for _, cmd := range extractHALCommands(content) {
		if cmd != "" && !seen[cmd] {
			seen[cmd] = true
			commands = append(commands, cmd)
		}
	}

	deprecatedCandidates := map[string]bool{}
	removedRe := regexp.MustCompile("(?i)`(hal\\s+[^`]+)`\\s+(?:has\\s+been\\s+removed|is\\s+deprecated)")
	for _, m := range removedRe.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 {
			cmd := normalizeDisplayCommand(strings.TrimSpace(m[1]))
			if strings.HasPrefix(cmd, "hal ") {
				deprecatedCandidates[cmd] = true
			}
		}
	}

	if len(deprecatedCandidates) == 0 {
		for _, cmd := range commands {
			if strings.Contains(lower, strings.ToLower(cmd)+" has been removed") || strings.Contains(lower, strings.ToLower(cmd)+" is deprecated") {
				deprecatedCandidates[cmd] = true
			}
		}
	}

	if len(deprecatedCandidates) == 0 {
		for _, cmd := range commands {
			if strings.Contains(cmd, " token") && (strings.Contains(lower, "removed") || strings.Contains(lower, "deprecated")) {
				deprecatedCandidates[cmd] = true
			}
		}
	}

	if len(deprecatedCandidates) == 0 {
		return deprecated
	}
	replacement := ""
	for _, cmd := range commands {
		if deprecatedCandidates[cmd] {
			continue
		}
		if strings.Contains(cmd, "workspace --enable") {
			replacement = cmd
			break
		}
	}
	if replacement == "" {
		for _, cmd := range commands {
			if strings.Contains(cmd, "deploy") || strings.Contains(cmd, "workspace") || strings.Contains(cmd, "status") {
				replacement = cmd
				break
			}
		}
	}
	if replacement == "" {
		replacement = "hal --help"
	}

	for cmd := range deprecatedCandidates {
		if strings.HasPrefix(cmd, "hal ") {
			deprecated[cmd] = replacement
		}
	}
	return deprecated
}

func mergeDeprecated(base map[string]string, incoming map[string]string) map[string]string {
	for k, v := range incoming {
		if k == "" {
			continue
		}
		base[k] = v
	}
	return base
}

func appendUnique(values []string, value string) []string {
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}
