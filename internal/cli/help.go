package cli

import (
	"fmt"
	"io"
	"strings"
)

// helpLang is the language of the help text. It never changes command output,
// JSON field names, error codes or exit codes.
type helpLang string

const (
	helpLangEN helpLang = "en"
	helpLangZH helpLang = "zh"

	// helpSchemaVersion is the stable schema of `help --json`.
	helpSchemaVersion = 1
)

func (l helpLang) String() string { return string(l) }

// normalizeHelpLanguage accepts en/zh with optional region and encoding, for
// example en, en-US, zh_CN.UTF-8. Other values are not a language selection.
func normalizeHelpLanguage(value string) (helpLang, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	switch {
	case normalized == "zh" || strings.HasPrefix(normalized, "zh-"):
		return helpLangZH, true
	case normalized == "en" || strings.HasPrefix(normalized, "en-"):
		return helpLangEN, true
	default:
		return "", false
	}
}

// resolveHelpLanguage chooses the help language and removes --lang from args.
//
// Priority: --lang, MEMCTL_LANG, the first non-empty LC_ALL/LC_MESSAGES/LANG,
// then English. An invalid MEMCTL_LANG falls through to the locale; a locale
// that is not Chinese selects English. An invalid --lang value is a usage
// error. --lang after `--` belongs to the wrapped command and is not consumed.
func resolveHelpLanguage(getenv func(string) string, args []string) (helpLang, []string, error) {
	lang := helpLangEN
	selected := false
	if value := getenv("MEMCTL_LANG"); strings.TrimSpace(value) != "" {
		if candidate, ok := normalizeHelpLanguage(value); ok {
			lang = candidate
			selected = true
		}
	}
	if !selected {
		// Locale precedence: the first non-empty LC_ALL, LC_MESSAGES or LANG
		// selects the language. A value that is not Chinese selects English, so
		// LC_ALL=C still overrides a lower-priority LANG=zh_CN.UTF-8.
		for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
			value := getenv(name)
			if strings.TrimSpace(value) == "" {
				continue
			}
			if candidate, ok := normalizeHelpLanguage(value); ok {
				lang = candidate
			} else {
				lang = helpLangEN
			}
			break
		}
	}

	cleaned := make([]string, 0, len(args))
	afterSeparator := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if afterSeparator {
			cleaned = append(cleaned, arg)
			continue
		}
		if arg == "--" {
			afterSeparator = true
			cleaned = append(cleaned, arg)
			continue
		}
		switch {
		case arg == "--lang":
			if index+1 >= len(args) {
				return lang, args, fmt.Errorf("--lang requires a value (en or zh)")
			}
			index++
			candidate, ok := normalizeHelpLanguage(args[index])
			if !ok {
				return lang, args, fmt.Errorf("invalid --lang %q: use en or zh", args[index])
			}
			lang = candidate
		case strings.HasPrefix(arg, "--lang="):
			value := strings.TrimPrefix(arg, "--lang=")
			candidate, ok := normalizeHelpLanguage(value)
			if !ok {
				return lang, args, fmt.Errorf("invalid --lang %q: use en or zh", value)
			}
			lang = candidate
		default:
			cleaned = append(cleaned, arg)
		}
	}
	return lang, cleaned, nil
}

// helpRequest reports whether args ask for help, and whether they also request
// the JSON form. Flags after `--` are ignored so a wrapped command keeps its
// own --help.
func helpRequest(args []string) (wantsHelp, wantsJSON bool) {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		switch arg {
		case "-h", "--help":
			wantsHelp = true
		case "--json":
			wantsJSON = true
		}
	}
	return wantsHelp, wantsJSON
}

// runHelp implements `help [command] [--json]`. It never reads stdin or the
// repository. --lang has already been removed by resolveHelpLanguage.
func runHelp(args []string, lang helpLang, stdout, stderr io.Writer) int {
	asJSON := false
	helpFlag := false
	topic := ""
	for _, arg := range args {
		switch {
		case arg == "--json":
			asJSON = true
		case arg == "-h" || arg == "--help":
			helpFlag = true
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(stderr, "memctl help: unknown flag %q\n", arg)
			return ExitUsageError
		case topic == "":
			topic = arg
		default:
			fmt.Fprintf(stderr, "memctl help: unexpected argument %q\n", arg)
			return ExitUsageError
		}
	}

	if topic == "" {
		if helpFlag {
			// `memctl help --help` shows the help command page; `memctl help`
			// and `memctl --help` show the overview.
			command, _ := lookupHelpCommand("help")
			return printCommandHelp(command, lang, asJSON, stdout, stderr)
		}
		if asJSON {
			if err := encodeJSON(stdout, buildRootHelpJSON(lang)); err != nil {
				return fail(stderr, "help", err)
			}
			return ExitOK
		}
		_, err := io.WriteString(stdout, renderRootHelp(lang))
		if err != nil {
			return fail(stderr, "help", err)
		}
		return ExitOK
	}

	command, ok := lookupHelpCommand(topic)
	if !ok {
		fmt.Fprintf(stderr, "memctl help: unknown command %q\n", topic)
		fmt.Fprintln(stderr, `Run "memctl help" for the command list.`)
		return ExitUsageError
	}
	return printCommandHelp(command, lang, asJSON, stdout, stderr)
}

// printCommandHelp writes one command page. It is shared by `help <command>`
// and `<command> --help`.
func printCommandHelp(command helpCommand, lang helpLang, asJSON bool, stdout, stderr io.Writer) int {
	if asJSON {
		if err := encodeJSON(stdout, buildCommandHelpJSON(command, lang)); err != nil {
			return fail(stderr, "help", err)
		}
		return ExitOK
	}
	_, err := io.WriteString(stdout, renderCommandHelp(command, lang))
	if err != nil {
		return fail(stderr, "help", err)
	}
	return ExitOK
}

// helpLabels are the localized section names. Colons stay ASCII so the text
// remains easy to split for agents.
type helpLabels struct {
	Usage       string
	Description string
	Arguments   string
	Flags       string
	Input       string
	Output      string
	ExitCodes   string
	Examples    string
	Notes       string
	Related     string
	Commands    string
	QuickStart  string
	Rules       string
	Environment string
	Language    string
	MoreHelp    string
	Required    string
	Optional    string
	Default     string
}

func helpLabelsFor(lang helpLang) helpLabels {
	if lang == helpLangZH {
		return helpLabels{
			Usage:       "用法",
			Description: "说明",
			Arguments:   "参数",
			Flags:       "选项",
			Input:       "输入",
			Output:      "输出",
			ExitCodes:   "退出码",
			Examples:    "示例",
			Notes:       "注意事项",
			Related:     "相关命令",
			Commands:    "命令",
			QuickStart:  "快速开始",
			Rules:       "使用规则",
			Environment: "环境变量",
			Language:    "语言",
			MoreHelp:    "更多帮助",
			Required:    "必填",
			Optional:    "可选",
			Default:     "默认值",
		}
	}
	return helpLabels{
		Usage:       "Usage",
		Description: "Description",
		Arguments:   "Arguments",
		Flags:       "Flags",
		Input:       "Input",
		Output:      "Output",
		ExitCodes:   "Exit codes",
		Examples:    "Examples",
		Notes:       "Notes",
		Related:     "Related",
		Commands:    "Commands",
		QuickStart:  "Quick start",
		Rules:       "Rules",
		Environment: "Environment",
		Language:    "Language",
		MoreHelp:    "More help",
		Required:    "required",
		Optional:    "optional",
		Default:     "Default",
	}
}

func renderRootHelp(lang helpLang) string {
	labels := helpLabelsFor(lang)
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n\n", helpRootData.Title.text(lang))
	fmt.Fprintf(&b, "%s\n\n", helpRootData.Summary.text(lang))

	fmt.Fprintf(&b, "%s:\n", labels.Usage)
	for _, line := range helpRootData.Usage {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "%s:\n", labels.QuickStart)
	for index, example := range helpRootData.QuickStart {
		fmt.Fprintf(&b, "  %d. %s\n", index+1, example.Command)
		if note := example.Note.text(lang); note != "" {
			fmt.Fprintf(&b, "     %s\n", note)
		}
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "%s:\n", labels.Rules)
	for _, rule := range helpRootData.Rules {
		fmt.Fprintf(&b, "  - %s\n", rule.text(lang))
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "%s:\n", labels.Commands)
	width := 0
	for _, command := range helpCommandData {
		if len(command.Name) > width {
			width = len(command.Name)
		}
	}
	for _, group := range helpRootData.Groups {
		fmt.Fprintf(&b, "  %s\n", group.Title.text(lang))
		for _, name := range group.Names {
			command, ok := lookupHelpCommand(name)
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "    %-*s  %s\n", width, command.Name, command.Summary.text(lang))
		}
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "%s:\n", labels.Environment)
	for _, env := range helpRootData.Environment {
		fmt.Fprintf(&b, "  %s\n", env.Name)
		line := env.Description.text(lang)
		if env.Default != "" {
			if lang == helpLangZH {
				line = fmt.Sprintf("%s: %s。%s", labels.Default, env.Default, line)
			} else {
				line = fmt.Sprintf("%s: %s. %s", labels.Default, env.Default, line)
			}
		}
		fmt.Fprintf(&b, "      %s\n", line)
	}
	b.WriteString("\n")

	b.WriteString(renderExitCodes(helpRootData.ExitCodes, labels, lang))

	fmt.Fprintf(&b, "%s:\n  %s\n\n", labels.Language, helpRootData.Language.text(lang))
	fmt.Fprintf(&b, "%s:\n  %s\n", labels.MoreHelp, helpRootData.MoreHelp.text(lang))
	return b.String()
}

func renderCommandHelp(command helpCommand, lang helpLang) string {
	labels := helpLabelsFor(lang)
	var b strings.Builder

	fmt.Fprintf(&b, "memctl %s - %s\n\n", command.Name, command.Summary.text(lang))

	fmt.Fprintf(&b, "%s:\n", labels.Usage)
	for _, line := range command.Usage {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "%s:\n  %s\n\n", labels.Description, command.Description.text(lang))

	if len(command.Arguments) > 0 {
		fmt.Fprintf(&b, "%s:\n", labels.Arguments)
		for _, item := range command.Arguments {
			fmt.Fprintf(&b, "  %s (%s)\n", item.displayName(), item.requiredLabel(labels))
			fmt.Fprintf(&b, "      %s\n", item.Description.text(lang))
		}
		b.WriteString("\n")
	}

	if len(command.Flags) > 0 {
		fmt.Fprintf(&b, "%s:\n", labels.Flags)
		for _, item := range command.Flags {
			fmt.Fprintf(&b, "  %s (%s)\n", item.displayName(), item.requiredLabel(labels))
			fmt.Fprintf(&b, "      %s\n", item.Description.text(lang))
		}
		b.WriteString("\n")
	}

	if len(command.Input) > 0 {
		fmt.Fprintf(&b, "%s:\n", labels.Input)
		for _, item := range command.Input {
			fmt.Fprintf(&b, "  - %s\n", item.text(lang))
		}
		b.WriteString("\n")
	}

	if len(command.Output) > 0 {
		fmt.Fprintf(&b, "%s:\n", labels.Output)
		for _, item := range command.Output {
			fmt.Fprintf(&b, "  - %s\n", item.text(lang))
		}
		b.WriteString("\n")
	}

	b.WriteString(renderExitCodes(command.ExitCodes, labels, lang))

	if len(command.Examples) > 0 {
		fmt.Fprintf(&b, "%s:\n", labels.Examples)
		for _, example := range command.Examples {
			fmt.Fprintf(&b, "  %s\n", example.Command)
			if note := example.Note.text(lang); note != "" {
				fmt.Fprintf(&b, "      %s\n", note)
			}
		}
		b.WriteString("\n")
	}

	if len(command.Notes) > 0 {
		fmt.Fprintf(&b, "%s:\n", labels.Notes)
		for _, note := range command.Notes {
			fmt.Fprintf(&b, "  - %s\n", note.text(lang))
		}
		b.WriteString("\n")
	}

	if len(command.Related) > 0 {
		fmt.Fprintf(&b, "%s:\n  %s\n", labels.Related, strings.Join(command.Related, ", "))
	}
	return b.String()
}

func renderExitCodes(codes []helpExit, labels helpLabels, lang helpLang) string {
	var b strings.Builder
	if len(codes) == 0 {
		return ""
	}
	fmt.Fprintf(&b, "%s:\n", labels.ExitCodes)
	for _, code := range codes {
		fmt.Fprintf(&b, "  %d  %s\n", code.Code, code.Meaning.text(lang))
	}
	b.WriteString("\n")
	return b.String()
}

func (item helpItem) displayName() string {
	if item.Value != "" {
		return item.Name + " " + item.Value
	}
	return item.Name
}

func (item helpItem) requiredLabel(labels helpLabels) string {
	if item.Required {
		return labels.Required
	}
	return labels.Optional
}

// JSON shapes. Keys and field names are stable English; only values are
// localized.
type helpJSON struct {
	SchemaVersion int               `json:"schemaVersion"`
	Tool          string            `json:"tool"`
	Language      string            `json:"language"`
	Title         string            `json:"title,omitempty"`
	Summary       string            `json:"summary,omitempty"`
	Usage         []string          `json:"usage,omitempty"`
	QuickStart    []helpExampleJSON `json:"quickStart,omitempty"`
	Rules         []string          `json:"rules,omitempty"`
	Commands      []helpCommandJSON `json:"commands,omitempty"`
	Environment   []helpEnvJSON     `json:"environment,omitempty"`
	ExitCodes     []helpExitJSON    `json:"exitCodes,omitempty"`
	LanguageHelp  string            `json:"languageHelp,omitempty"`
	MoreHelp      string            `json:"moreHelp,omitempty"`
}

type helpCommandJSON struct {
	Name        string            `json:"name"`
	Summary     string            `json:"summary"`
	Usage       []string          `json:"usage"`
	Description string            `json:"description"`
	Arguments   []helpItemJSON    `json:"arguments,omitempty"`
	Flags       []helpItemJSON    `json:"flags,omitempty"`
	Input       []string          `json:"input,omitempty"`
	Output      []string          `json:"output,omitempty"`
	ExitCodes   []helpExitJSON    `json:"exitCodes"`
	Examples    []helpExampleJSON `json:"examples,omitempty"`
	Notes       []string          `json:"notes,omitempty"`
	Related     []string          `json:"related,omitempty"`
}

type helpItemJSON struct {
	Name        string `json:"name"`
	Value       string `json:"value,omitempty"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

type helpExampleJSON struct {
	Command string `json:"command"`
	Note    string `json:"note,omitempty"`
}

type helpEnvJSON struct {
	Name        string `json:"name"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description"`
}

type helpExitJSON struct {
	Code    int    `json:"code"`
	Meaning string `json:"meaning"`
}

func buildRootHelpJSON(lang helpLang) helpJSON {
	payload := helpJSON{
		SchemaVersion: helpSchemaVersion,
		Tool:          "memctl",
		Language:      lang.String(),
		Title:         helpRootData.Title.text(lang),
		Summary:       helpRootData.Summary.text(lang),
		Usage:         helpRootData.Usage,
		Rules:         localizeTexts(helpRootData.Rules, lang),
		ExitCodes:     localizedExitCodes(helpRootData.ExitCodes, lang),
		LanguageHelp:  helpRootData.Language.text(lang),
		MoreHelp:      helpRootData.MoreHelp.text(lang),
	}
	for _, example := range helpRootData.QuickStart {
		payload.QuickStart = append(payload.QuickStart, helpExampleJSON{
			Command: example.Command,
			Note:    example.Note.text(lang),
		})
	}
	for _, env := range helpRootData.Environment {
		payload.Environment = append(payload.Environment, helpEnvJSON{
			Name:        env.Name,
			Default:     env.Default,
			Description: env.Description.text(lang),
		})
	}
	for _, command := range helpCommandData {
		payload.Commands = append(payload.Commands, buildCommandHelpItemJSON(command, lang))
	}
	return payload
}

func buildCommandHelpJSON(command helpCommand, lang helpLang) helpJSON {
	return helpJSON{
		SchemaVersion: helpSchemaVersion,
		Tool:          "memctl",
		Language:      lang.String(),
		Commands:      []helpCommandJSON{buildCommandHelpItemJSON(command, lang)},
	}
}

func buildCommandHelpItemJSON(command helpCommand, lang helpLang) helpCommandJSON {
	payload := helpCommandJSON{
		Name:        command.Name,
		Summary:     command.Summary.text(lang),
		Usage:       command.Usage,
		Description: command.Description.text(lang),
		Input:       localizeTexts(command.Input, lang),
		Output:      localizeTexts(command.Output, lang),
		ExitCodes:   localizedExitCodes(command.ExitCodes, lang),
		Notes:       localizeTexts(command.Notes, lang),
		Related:     command.Related,
	}
	for _, item := range command.Arguments {
		payload.Arguments = append(payload.Arguments, helpItemJSON{
			Name:        item.Name,
			Value:       item.Value,
			Required:    item.Required,
			Description: item.Description.text(lang),
		})
	}
	for _, item := range command.Flags {
		payload.Flags = append(payload.Flags, helpItemJSON{
			Name:        item.Name,
			Value:       item.Value,
			Required:    item.Required,
			Description: item.Description.text(lang),
		})
	}
	for _, example := range command.Examples {
		payload.Examples = append(payload.Examples, helpExampleJSON{
			Command: example.Command,
			Note:    example.Note.text(lang),
		})
	}
	return payload
}

func localizeTexts(items []localizedText, lang helpLang) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.text(lang))
	}
	return out
}

func localizedExitCodes(codes []helpExit, lang helpLang) []helpExitJSON {
	if len(codes) == 0 {
		return nil
	}
	out := make([]helpExitJSON, 0, len(codes))
	for _, code := range codes {
		out = append(out, helpExitJSON{Code: code.Code, Meaning: code.Meaning.text(lang)})
	}
	return out
}
