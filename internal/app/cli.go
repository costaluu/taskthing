package app

import (
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/costaluu/taskthing/internal/js"
)

// ── the command-line surface (what Bunli did for the TS binary) ─────────────
//
// Routing, option parsing and validation, help, and the error envelopes are
// reproduced from @bunli/core 0.9: piped, everything it prints is a JSON
// envelope; on a terminal, plain text. Scripts that read those keep working.

// OptionKind is the zod schema an option was declared with.
type OptionKind int

const (
	CoerceBool  OptionKind = iota // z.coerce.boolean().default(false)
	StrictBool                    // z.boolean().default(false) — the global flags
	String                        // z.string().optional()
	StringDflt                    // z.string().default("")
	Enum                          // z.enum([...]).optional()
	PositiveInt                   // z.coerce.number().int().min(1)
)

type Option struct {
	Name        string
	Short       string
	Description string
	Flag        bool // argumentKind: "flag"
	Kind        OptionKind
	Values      []string
}

// expectedType is extractSchemaType: the zod schema's `type`.
func (o Option) expectedType() string {
	switch o.Kind {
	case CoerceBool, StrictBool, StringDflt:
		return "default"
	case String, Enum:
		return "optional"
	}
	return "number"
}

type Command struct {
	Name        string
	Description string
	Options     []Option
	Commands    []*Command
	Handler     func(ctx *Ctx) error
}

// Ctx is what a handler receives.
type Ctx struct {
	Flags      map[string]any
	Positional []string
}

func (c *Ctx) Bool(name string) bool { b, _ := c.Flags[name].(bool); return b }

// Str is an optional string flag; "" when absent.
func (c *Ctx) Str(name string) (string, bool) {
	s, ok := c.Flags[name].(string)
	return s, ok
}

var globalFlags = []Option{
	{Name: "help", Short: "h", Description: "Show help", Flag: true, Kind: StrictBool},
	{Name: "version", Short: "v", Description: "Show version", Flag: true, Kind: StrictBool},
	{Name: "image-mode", Description: "Terminal image preview mode (off|auto|on)", Kind: Enum, Values: []string{"off", "auto", "on"}},
	{Name: "format", Description: "Output format (json|yaml|md|toon)", Kind: Enum, Values: []string{"json", "yaml", "md", "toon"}},
	{Name: "llms", Description: "Print compact command manifest (Markdown)", Flag: true, Kind: StrictBool},
	{Name: "llms-full", Description: "Print full command manifest (Markdown)", Flag: true, Kind: StrictBool},
}

// ── errors ───────────────────────────────────────────────────────────────────

// ValidationError is BunliValidationError.
type ValidationError struct {
	Message, Command, Option, ExpectedType, Hint string
}

func (e *ValidationError) Error() string { return e.Message }

// NotFoundError is CommandNotFoundError.
type NotFoundError struct {
	Message, Command, Suggestion string
	Available                    []string
}

func (e *NotFoundError) Error() string { return e.Message }

// ExecutionError is CommandExecutionError: a handler that threw.
type ExecutionError struct{ Message, Command string }

func (e *ExecutionError) Error() string { return e.Message }

func serializeError(err error) *js.Object {
	var v *ValidationError
	var n *NotFoundError
	var x *ExecutionError
	switch {
	case errors.As(err, &v):
		o := js.Obj("kind", "validation", "name", "BunliValidationError", "tag", "BunliValidationError",
			"message", v.Message, "command", v.Command, "option", v.Option, "expectedType", v.ExpectedType)
		if v.Hint != "" {
			o.Set("hint", v.Hint)
		}
		return o
	case errors.As(err, &n):
		available := make([]any, len(n.Available))
		for i, a := range n.Available {
			available[i] = a
		}
		o := js.Obj("kind", "command-not-found", "name", "CommandNotFoundError", "tag", "CommandNotFoundError",
			"message", n.Message, "command", n.Command, "available", available)
		if n.Suggestion != "" {
			o.Set("suggestion", n.Suggestion)
		}
		return o
	case errors.As(err, &x):
		return js.Obj("kind", "command-execution", "name", "CommandExecutionError", "tag", "CommandExecutionError",
			"message", x.Message, "command", x.Command)
	}
	return js.Obj("kind", "error", "name", "Error", "message", err.Error())
}

// ── option parsing (parser.ts) ───────────────────────────────────────────────

func validateOption(o Option, value *string, command string) (any, error) {
	var processed any
	if value != nil {
		processed = *value
		// "true"/"false" become booleans when the schema would take one.
		if *value == "true" || *value == "false" {
			if o.Kind == CoerceBool || o.Kind == StrictBool || o.Kind == PositiveInt {
				processed = *value == "true"
			}
		}
	}
	fail := func(message string) error {
		hint := ""
		if value != nil && o.Kind == PositiveInt {
			hint = "Provide a numeric value"
		}
		return &ValidationError{
			Message: fmt.Sprintf("Invalid option '%s': %s", o.Name, message), Command: command,
			Option: o.Name, ExpectedType: o.expectedType(), Hint: hint,
		}
	}
	switch o.Kind {
	case CoerceBool:
		switch v := processed.(type) {
		case nil:
			return false, nil
		case bool:
			return v, nil
		case string:
			return v != "", nil
		}
	case StrictBool:
		switch v := processed.(type) {
		case nil:
			return false, nil
		case bool:
			return v, nil
		}
		return nil, fail("Invalid input: expected boolean, received string")
	case String:
		if processed == nil {
			return nil, nil
		}
		return processed, nil
	case StringDflt:
		if processed == nil {
			return "", nil
		}
		return processed, nil
	case Enum:
		if processed == nil {
			return nil, nil
		}
		if s, ok := processed.(string); ok && slices.Contains(o.Values, s) {
			return s, nil
		}
		quoted := make([]string, len(o.Values))
		for i, v := range o.Values {
			quoted[i] = js.Quote(v)
		}
		return nil, fail("Invalid option: expected one of " + strings.Join(quoted, "|"))
	case PositiveInt:
		var n float64
		switch v := processed.(type) {
		case nil:
			n = math.NaN()
		case bool:
			if v {
				n = 1
			}
		case string:
			n = js.ToNumber(v)
		}
		if math.IsNaN(n) {
			return nil, fail("Invalid input: expected number, received NaN")
		}
		if !js.IsInteger(n) {
			return nil, fail("Invalid input: expected int, received number")
		}
		if n < 1 {
			return nil, fail("Too small: expected number to be >=1")
		}
		return int(n), nil
	}
	return processed, nil
}

func findOption(options []Option, name string) (Option, bool) {
	for _, o := range options {
		if o.Name == name {
			return o, true
		}
	}
	return Option{}, false
}

func parseArgs(args []string, options []Option, command string) (map[string]any, []string, error) {
	flags := map[string]any{}
	var positional []string
	stop := false
	takesNext := func(o Option, i int) bool {
		return i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") &&
			(!o.Flag || args[i+1] == "true" || args[i+1] == "false")
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		if arg == "--" {
			stop = true
			continue
		}
		if stop {
			positional = append(positional, arg)
			continue
		}
		switch {
		case strings.HasPrefix(arg, "--"):
			eq := strings.Index(arg, "=")
			name := arg[2:]
			var inline *string
			if eq > 0 {
				name = arg[2:eq]
				v := arg[eq+1:]
				inline = &v
			}
			o, ok := findOption(options, name)
			if name == "" || !ok {
				continue
			}
			value := inline
			if value == nil && takesNext(o, i) {
				i++
				value = &args[i]
			}
			if value == nil && o.Flag {
				t := "true"
				value = &t
			}
			v, err := validateOption(o, value, command)
			if err != nil {
				return nil, nil, err
			}
			flags[name] = v
		case strings.HasPrefix(arg, "-") && len(arg) > 1:
			short := arg[1:]
			var o Option
			found := false
			for _, opt := range options {
				if opt.Short != "" && opt.Short == short {
					o, found = opt, true
				}
			}
			if !found {
				continue
			}
			var value *string
			if takesNext(o, i) {
				i++
				value = &args[i]
			}
			if value == nil && o.Flag {
				t := "true"
				value = &t
			}
			v, err := validateOption(o, value, command)
			if err != nil {
				return nil, nil, err
			}
			flags[o.Name] = v
		default:
			positional = append(positional, arg)
		}
	}
	for _, o := range options {
		if _, ok := flags[o.Name]; !ok {
			v, err := validateOption(o, nil, command)
			if err != nil {
				return nil, nil, err
			}
			flags[o.Name] = v
		}
	}
	return flags, positional, nil
}

// ── the CLI ──────────────────────────────────────────────────────────────────

type CLI struct {
	Name, Version, Description string
	top                        []*Command
	commands                   map[string]*Command
}

func NewCLI(name, version, description string) *CLI {
	return &CLI{Name: name, Version: version, Description: description, commands: map[string]*Command{}}
}

func (c *CLI) Register(cmd *Command) {
	c.top = append(c.top, cmd)
	c.register(cmd, nil)
}

func (c *CLI) register(cmd *Command, path []string) {
	full := append(append([]string(nil), path...), cmd.Name)
	c.commands[strings.Join(full, " ")] = cmd
	for _, sub := range cmd.Commands {
		c.register(sub, full)
	}
}

func (c *CLI) findCommand(args []string) (*Command, []string) {
	for i := len(args); i > 0; i-- {
		if cmd, ok := c.commands[strings.Join(args[:i], " ")]; ok {
			return cmd, args[i:]
		}
	}
	return nil, args
}

func (c *CLI) available() []string {
	var names []string
	for name := range c.commands {
		if !strings.Contains(name, " ") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// stripGlobals drops recognized global flags (and their values), keeping the
// original index of every remaining argument.
func stripGlobals(args []string) ([]string, []int) {
	var out []string
	var indexes []int
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		var flag *Option
		if strings.HasPrefix(arg, "--") {
			name := arg[2:]
			eq := strings.Index(arg, "=")
			if eq > 0 {
				name = arg[2:eq]
			}
			if o, ok := findOption(globalFlags, name); ok && name != "" {
				flag = &o
				if eq < 0 && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && (!o.Flag || args[i+1] == "true" || args[i+1] == "false") {
					i++
				}
				continue
			}
		} else if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			for _, o := range globalFlags {
				if o.Short != "" && o.Short == arg[1:] {
					flag = &o
				}
			}
			if flag != nil {
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && (!flag.Flag || args[i+1] == "true" || args[i+1] == "false") {
					i++
				}
				continue
			}
		}
		out = append(out, arg)
		indexes = append(indexes, i)
	}
	return out, indexes
}

func explicitFormat(args []string) string {
	format := ""
	valid := []string{"json", "yaml", "md", "toon"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--format=") {
			if v := arg[len("--format="):]; slices.Contains(valid, v) {
				format = v
			}
			continue
		}
		if arg == "--format" && i+1 < len(args) && slices.Contains(valid, args[i+1]) {
			format = args[i+1]
			i++
		}
	}
	return format
}

// resolveFormat: an explicit --format wins; piped defaults to JSON, a terminal to text.
func resolveFormat(flag string) string {
	if flag != "" {
		return flag
	}
	if !stdoutTTY {
		return "json"
	}
	return "toon"
}

// Run is cli.run(argv): it returns the process exit code.
func (c *CLI) Run(argv []string) int {
	if len(argv) == 0 {
		c.printHelp(resolveFormat(""), nil, nil)
		return 0
	}
	commandArgs, passthrough := argv, []string(nil)
	if i := slices.Index(argv, "--"); i >= 0 {
		commandArgs, passthrough = argv[:i], argv[i+1:]
	}
	lookup, indexes := stripGlobals(commandArgs)
	command, remaining := c.findCommand(lookup)
	explicit := explicitFormat(commandArgs)

	globals := globalFlags
	if command != nil {
		var kept []Option
		for _, g := range globalFlags {
			if _, clash := findOption(command.Options, g.Name); !clash || slices.Contains([]string{"help", "version", "llms", "llms-full"}, g.Name) {
				kept = append(kept, g)
			}
		}
		globals = kept
	}
	globalParsed, _, err := parseArgs(commandArgs, globals, "__global__")
	if err != nil {
		c.printError(err, resolveFormat(explicit))
		return 1
	}
	format, _ := globalParsed["format"].(string)
	if format == "" {
		format = explicit
	}
	output := resolveFormat(format)

	if b, _ := globalParsed["version"].(bool); b {
		c.printVersion(output)
		return 0
	}
	if b, _ := globalParsed["llms-full"].(bool); b {
		c.printManifest(true, output, format != "")
		return 0
	}
	if b, _ := globalParsed["llms"].(bool); b {
		c.printManifest(false, output, format != "")
		return 0
	}
	if b, _ := globalParsed["help"].(bool); b {
		cmdArgs, _ := stripGlobals(commandArgs)
		if len(cmdArgs) == 0 {
			c.printHelp(output, nil, nil)
			return 0
		}
		cmd, rest := c.findCommand(cmdArgs)
		if cmd == nil {
			c.printError(&NotFoundError{
				Message: fmt.Sprintf("Command '%s' not found", strings.Join(cmdArgs, " ")),
				Command: strings.Join(cmdArgs, " "), Available: c.available(),
			}, output)
			return 1
		}
		matched := cmdArgs[:len(cmdArgs)-len(rest)]
		c.printHelp(output, cmd, matched[:len(matched)-1])
		return 0
	}
	if command == nil {
		input := ""
		if len(lookup) > 0 {
			input = lookup[0]
		}
		suggestion := findSuggestion(input, c.available())
		message := fmt.Sprintf("Command '%s' not found", input)
		if suggestion != "" {
			message += fmt.Sprintf(". Did you mean '%s'?", suggestion)
		}
		c.printError(&NotFoundError{Message: message, Command: input, Available: c.available(), Suggestion: suggestion}, output)
		return 1
	}

	matchedLen := len(lookup) - len(remaining)
	matched := map[int]bool{}
	for _, i := range indexes[:matchedLen] {
		matched[i] = true
	}
	var all []string
	for i, a := range commandArgs {
		if !matched[i] {
			all = append(all, a)
		}
	}
	all = append(all, passthrough...)

	options := append(append([]Option(nil), globalFlags...), command.Options...)
	flags, positional, err := parseArgs(all, options, command.Name)
	if err == nil {
		err = command.Handler(&Ctx{Flags: flags, Positional: positional})
		var exit *exitError
		if errors.As(err, &exit) {
			// process.exit: no finally runs, only ink's exit hook restores the cursor.
			if inkMounted {
				os.Stdout.WriteString("\x1b[?25h")
			}
			return exit.code
		}
		if stdoutTTY {
			// Bunli's prompt session restores the terminal's modes on its way out.
			os.Stdout.WriteString("\x1b[?1004l\x1b[?2004l\x1b[?1006l\x1b[?1003l\x1b[?1002l\x1b[?1000l\x1b[<u\x1b[>4;0m\x1b[?25h\x1b[?1004l")
		}
		if err != nil {
			var v *ValidationError
			if !errors.As(err, &v) {
				err = &ExecutionError{Message: err.Error(), Command: command.Name}
			}
		}
	}
	if err != nil {
		if stdoutTTY && flags == nil {
			// The options never parsed, so there was no prompt session to
			// dispose — only the focus-reporting reset runs on the way out.
			os.Stdout.WriteString("\x1b[?1004l")
		}
		c.printError(err, output)
		return 1
	}
	return exitCode
}

// exitError carries a process.exit from deep inside a handler.
type exitError struct{ code int }

func (e *exitError) Error() string { return "exit " + strconv.Itoa(e.code) }

// exitCode is process.exitCode: set by a verb that failed but carried on.
var exitCode = 0

func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}
	row := make([]int, len(br)+1)
	for i := range row {
		row[i] = i
	}
	for i := 1; i <= len(ar); i++ {
		prev := i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			val := min(row[j]+1, prev+1, row[j-1]+cost)
			row[j-1] = prev
			prev = val
		}
		row[len(br)] = prev
	}
	return row[len(br)]
}

func findSuggestion(input string, candidates []string) string {
	if input == "" {
		return ""
	}
	best, bestDistance := "", 0
	for _, c := range candidates {
		if strings.HasPrefix(c, input) || strings.HasPrefix(input, c) {
			return c
		}
		if d := levenshtein(input, c); d <= 3 && (best == "" || d < bestDistance) {
			best, bestDistance = c, d
		}
	}
	return best
}

// ── help and version ─────────────────────────────────────────────────────────

func terminalWidth() int {
	if w := stdoutColumns(); w > 0 {
		return w
	}
	return 80
}

func wrapText(text string, width int) []string {
	width = max(10, width)
	words := strings.Fields(strings.TrimSpace(text))
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len([]rune(line+" "+w)) <= width {
			line += " " + w
		} else {
			lines = append(lines, line)
			line = w
		}
	}
	return append(lines, line)
}

type row struct{ label, description string }

func formatTwoColumnRows(rows []row, width int) []string {
	maxLabel := 0
	for _, r := range rows {
		maxLabel = max(maxLabel, len([]rune(r.label)))
	}
	labelWidth := min(maxLabel+2, max(18, width*4/10))
	descWidth := max(20, width-2-labelWidth-1)
	var out []string
	for _, r := range rows {
		lines := wrapText(r.description, descWidth)
		if len([]rune(r.label)) >= labelWidth-1 {
			out = append(out, "  "+r.label)
			for _, l := range lines {
				out = append(out, "  "+strings.Repeat(" ", labelWidth)+l)
			}
			continue
		}
		for i, l := range lines {
			if i == 0 {
				out = append(out, "  "+padEnd(r.label, labelWidth)+l)
			} else {
				out = append(out, "  "+strings.Repeat(" ", labelWidth)+l)
			}
		}
	}
	return out
}

func padEnd(s string, n int) string {
	if pad := n - len([]rune(s)); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

func (c *CLI) helpText(cmd *Command, path []string) string {
	width := terminalWidth()
	var lines []string
	if cmd == nil {
		lines = append(lines, fmt.Sprintf("%s v%s", c.Name, c.Version))
		if c.Description != "" {
			lines = append(lines, c.Description)
		}
		if len(c.top) > 0 {
			lines = append(lines, "", "Commands:")
			var rows []row
			for _, t := range c.sortedTop() {
				rows = append(rows, row{t.Name, t.Description})
			}
			lines = append(lines, formatTwoColumnRows(rows, width)...)
		}
		return strings.Join(lines, "\n")
	}
	full := strings.Join(append(append([]string(nil), path...), cmd.Name), " ")
	lines = append(lines, fmt.Sprintf("Usage: %s %s [options]", c.Name, full), "", cmd.Description)
	if len(cmd.Options) > 0 {
		lines = append(lines, "", "Options:")
		var rows []row
		for _, o := range cmd.Options {
			label := "--" + o.Name
			if o.Short != "" {
				label += ", -" + o.Short
			}
			rows = append(rows, row{label, o.Description})
		}
		lines = append(lines, formatTwoColumnRows(rows, width)...)
	}
	if len(cmd.Commands) > 0 {
		lines = append(lines, "", "Subcommands:")
		var rows []row
		for _, s := range cmd.Commands {
			rows = append(rows, row{s.Name, s.Description})
		}
		lines = append(lines, formatTwoColumnRows(rows, width)...)
	}
	return strings.Join(lines, "\n")
}

// sortedTop is the registration map's top-level commands, in the order a Map
// keyed by full path yields them: registration order.
func (c *CLI) sortedTop() []*Command { return c.top }

func (c *CLI) printHelp(format string, cmd *Command, path []string) {
	text := c.helpText(cmd, path)
	if format == "toon" {
		consoleLog(text)
		return
	}
	full := []any{}
	for _, p := range path {
		full = append(full, p)
	}
	if cmd != nil {
		full = append(full, cmd.Name)
	}
	writeFormatted(os.Stdout, js.Obj("ok", true, "data", js.Obj(
		"type", "help", "cliName", c.Name, "version", c.Version, "path", full, "text", text,
	)), format)
}

func (c *CLI) printVersion(format string) {
	if format == "toon" {
		os.Stdout.WriteString(fmt.Sprintf("%s v%s\n", c.Name, c.Version))
		return
	}
	writeFormatted(os.Stdout, js.Obj("ok", true, "data", js.Obj("type", "version", "name", c.Name, "version", c.Version)), format)
}

func (c *CLI) printError(err error, format string) {
	if format != "toon" {
		writeFormatted(os.Stderr, js.Obj("ok", false, "error", serializeError(err)), format)
		return
	}
	consoleError(bunliRed("Error: " + err.Error()))
	var v *ValidationError
	if errors.As(err, &v) && v.Hint != "" {
		consoleError(bunliDim("Hint: " + v.Hint))
	}
}

func writeFormatted(w *os.File, value *js.Object, format string) {
	var text string
	switch format {
	case "json":
		text = js.StringifyIndent(value, 2)
	case "yaml":
		text = strings.TrimSuffix(yamlStringify(value, 0), "\n")
	case "md":
		text = formatMarkdown(value, nil)
	}
	if text != "" {
		w.WriteString(text + "\n")
	}
}

// manifest is --llms (an index table) / --llms-full (every command's section).
func (c *CLI) manifest(full bool) string {
	signature := func(name string, cmd *Command) string {
		base := c.Name + " " + name
		if len(cmd.Options) == 0 {
			return base
		}
		return base + " [options]"
	}
	if !full {
		lines := []string{"# " + c.Name}
		if c.Description != "" {
			lines = append(lines, "", c.Description)
		}
		lines = append(lines, "", "| Command | Description |", "|---------|-------------|")
		for _, t := range c.top {
			lines = append(lines, fmt.Sprintf("| `%s` | %s |", signature(t.Name, t), t.Description))
		}
		lines = append(lines, "", fmt.Sprintf("Run `%s --llms-full` for full manifest. Run `%s <command> --help` for usage details.", c.Name, c.Name))
		return strings.Join(lines, "\n")
	}
	sections := []string{"# " + c.Name}
	if c.Description != "" {
		sections = append(sections, c.Description)
	}
	var collect func(name string, cmd *Command)
	collect = func(name string, cmd *Command) {
		heading := "## " + c.Name + " " + name
		if cmd.Description != "" {
			heading += "\n\n" + cmd.Description
		}
		parts := []string{heading}
		if len(cmd.Options) > 0 {
			var rows []string
			for _, o := range cmd.Options {
				flagName := "--" + o.Name
				if o.Short != "" {
					flagName += ", -" + o.Short
				}
				def := ""
				if o.Kind == CoerceBool || o.Kind == StrictBool {
					def = "`false`"
				}
				rows = append(rows, fmt.Sprintf("| `%s` | `%s` | %s | %s |", flagName, o.jsonType(), def, o.Description))
			}
			parts = append(parts, "### Options\n\n| Flag | Type | Default | Description |\n|------|------|---------|-------------|\n"+strings.Join(rows, "\n"))
		}
		if len(cmd.Commands) > 0 {
			var rows []string
			for _, sub := range cmd.Commands {
				rows = append(rows, fmt.Sprintf("| `%s` | %s |", sub.Name, sub.Description))
			}
			parts = append(parts, "### Subcommands\n\n| Command | Description |\n|---------|-------------|\n"+strings.Join(rows, "\n"))
		}
		sections = append(sections, strings.Join(parts, "\n\n"))
		for _, sub := range cmd.Commands {
			collect(name+" "+sub.Name, sub)
		}
	}
	for _, t := range c.top {
		collect(t.Name, t)
	}
	return strings.Join(sections, "\n\n")
}

// jsonType is the option's JSON-schema type as the manifest names it.
func (o Option) jsonType() string {
	switch o.Kind {
	case CoerceBool, StrictBool:
		return "boolean"
	case Enum:
		return strings.Join(o.Values, " | ")
	case PositiveInt:
		return "number"
	}
	return "string"
}

func (c *CLI) printManifest(full bool, format string, explicit bool) {
	markdown := c.manifest(full)
	if !explicit || format == "md" || format == "toon" {
		os.Stdout.WriteString(markdown + "\n")
		return
	}
	variant := "compact"
	if full {
		variant = "full"
	}
	writeFormatted(os.Stdout, js.Obj("ok", true, "data", js.Obj("type", "manifest", "variant", variant, "markdown", markdown)), format)
}
