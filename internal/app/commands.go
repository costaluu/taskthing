package app

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/costaluu/taskthing/internal/js"
	"github.com/costaluu/taskthing/internal/mdwal"
	"github.com/costaluu/taskthing/internal/nodefs"
	"github.com/costaluu/taskthing/internal/rrule"
)

func workspaceOption() Option {
	return Option{Name: "workspace", Description: "the workspace to act on (default: the current one)", Kind: String}
}

func flag(name, description string) Option {
	return Option{Name: name, Description: description, Flag: true, Kind: CoerceBool}
}

func workspaceFlag(c *Ctx) string { s, _ := c.Str("workspace"); return s }

// rejectUnknownSubcommand is the handler a pure group carries, so a typo'd
// subcommand fails instead of printing help with a zero exit.
func rejectUnknownSubcommand(message func(string) string) func(*Ctx) error {
	return func(c *Ctx) error {
		typed := ""
		if len(c.Positional) > 0 {
			typed = c.Positional[0]
		}
		return errors.New(message(typed))
	}
}

func first(positional []string) string {
	if len(positional) > 0 {
		return positional[0]
	}
	return ""
}

// BuildCLI is the command tree, registered in the order the TS app did.
func BuildCLI() *CLI {
	cli := NewCLI("taskthing", Version, "a git-based task manager")
	for _, cmd := range []*Command{
		addCommand(), listCommand(),
		taskVerb("check", "complete a task", func(ref, ws string) error { return completeTask(ref, true, ws) }),
		taskVerb("uncheck", "reopen a task", func(ref, ws string) error { return completeTask(ref, false, ws) }),
		taskVerb("star", "star a task", func(ref, ws string) error { return starTask(ref, true, ws) }),
		taskVerb("unstar", "remove a task's star", func(ref, ws string) error { return starTask(ref, false, ws) }),
		taskVerb("delete", "soft-delete a task", func(ref, ws string) error { return setTaskDeleted(ref, true, ws) }),
		taskVerb("recover", "restore a deleted task", func(ref, ws string) error { return setTaskDeleted(ref, false, ws) }),
		taskVerb("clear", "remove an already-deleted task from this machine", clearTask),
		setCommand(),
		boardCommand(),
		{Name: "boards", Description: "list boards (shortcut for `board list`)", Options: []Option{workspaceOption()},
			Handler: func(c *Ctx) error { return listBoards(workspaceFlag(c)) }},
		workspaceCommand(),
		{Name: "workspaces", Description: "list workspaces (shortcut for `workspace list`)",
			Handler: func(*Ctx) error { return listWorkspaces() }},
		{Name: "use", Description: "switch the current workspace (shortcut for `workspace use`)",
			Handler: func(c *Ctx) error { return useWorkspace(first(c.Positional)) }},
		syncVerb("sync", "publish this peer's events to the remote", syncCopy, func(m *manager) error { return m.sync() }),
		syncVerb("pull", "replay peers' events into this workspace", pullCopy, func(m *manager) error { return m.pull() }),
		syncVerb("rebuild", "re-derive this workspace from the consolidated snapshot", rebuildCopy, func(m *manager) error { return m.rebuild() }),
		truncateCommand(),
		configCommand(),
		themeCommand(),
		updateCommand(),
		migrationsCommand(),
		installCommand(),
		entityCommand(),
	} {
		cli.Register(cmd)
	}
	return cli
}

// ── tasks ────────────────────────────────────────────────────────────────────

// onTask resolves what the user typed to a task, opens its workspace, and
// reports what edit did under `verb`.
func onTask(verb, reference, workspace string, edit func(task *js.Object, tasks repository, m *mdwal.Mdwal, root string, config *js.Object) (taskOutcome, error)) error {
	return reportTask(verb, func(config *js.Object) (taskOutcome, error) {
		root, id, err := resolveTask(reference, workspace)
		if err != nil {
			return taskOutcome{}, err
		}
		m, _, err := openWorkspace(root)
		if err != nil {
			return taskOutcome{}, err
		}
		tasks := taskRepository(m)
		task := tasks.findByID(id)
		if task == nil {
			return taskOutcome{}, fmt.Errorf("no such task: %s", reference)
		}
		return edit(task, tasks, m, root, config)
	})
}

func taskVerb(name, description string, act func(ref, ws string) error) *Command {
	return &Command{Name: name, Description: description, Options: []Option{workspaceOption()},
		Handler: func(c *Ctx) error {
			return eachReference(c.Positional, func(ref string) error { return act(ref, workspaceFlag(c)) })
		}}
}

func completeTask(reference string, value bool, workspace string) error {
	verb := "reopen"
	if value {
		verb = "complete"
	}
	return onTask(verb, reference, workspace, func(task *js.Object, tasks repository, _ *mdwal.Mdwal, _ string, config *js.Object) (taskOutcome, error) {
		if _, err := tasks.update(with(task, "completed", value)); err != nil {
			return taskOutcome{}, err
		}
		title := str(task, "title")
		if !value {
			return taskOutcome{title: title, predicate: "reopened"}, nil
		}
		// A recurring task lives on as a new occurrence inheriting the rule.
		if rule := strPtr(task, "rrule"); rule != nil {
			next, ok, err := rrule.NextOccurrence(*rule)
			if err != nil {
				return taskOutcome{}, err
			}
			if ok {
				fields := task.Clone()
				fields.Delete("id")
				if _, err := tasks.create(with(fields, "completed", false, "rrule", next)); err != nil {
					return taskOutcome{}, err
				}
				return datedOutcome(title, "completed", &next, config)
			}
		}
		return taskOutcome{title: title, predicate: "completed"}, nil
	})
}

func starTask(reference string, value bool, workspace string) error {
	verb, predicate := "unstar", "unstarred"
	if value {
		verb, predicate = "star", "starred"
	}
	return onTask(verb, reference, workspace, func(task *js.Object, tasks repository, _ *mdwal.Mdwal, _ string, _ *js.Object) (taskOutcome, error) {
		if _, err := tasks.update(with(task, "star", value)); err != nil {
			return taskOutcome{}, err
		}
		return taskOutcome{title: str(task, "title"), predicate: predicate}, nil
	})
}

func setTaskDeleted(reference string, value bool, workspace string) error {
	verb, predicate := "recover", "recovered"
	if value {
		verb, predicate = "delete", "deleted"
	}
	return onTask(verb, reference, workspace, func(task *js.Object, tasks repository, _ *mdwal.Mdwal, _ string, _ *js.Object) (taskOutcome, error) {
		done, err := tasks.setDeleted(str(task, "id"), value)
		if err != nil {
			return taskOutcome{}, err
		}
		if !done {
			return taskOutcome{}, fmt.Errorf("no such task: %s", reference)
		}
		return taskOutcome{title: str(task, "title"), predicate: predicate}, nil
	})
}

func clearTask(reference, workspace string) error {
	return onTask("permanently delete", reference, workspace, func(task *js.Object, _ repository, m *mdwal.Mdwal, _ string, _ *js.Object) (taskOutcome, error) {
		if !boolean(task, "deleted") {
			return taskOutcome{}, fmt.Errorf("task %s is not deleted — `delete` it first", reference)
		}
		if err := m.Discard("task", str(task, "id")); err != nil {
			return taskOutcome{}, err
		}
		return taskOutcome{title: str(task, "title"), predicate: "permanently deleted"}, nil
	})
}

func addCommand() *Command {
	return &Command{Name: "add", Description: "add a task (d:[…] date, r:[…] recurrence, b:[…] board)", Options: []Option{workspaceOption()},
		Handler: func(c *Ctx) error {
			input := strings.Join(c.Positional, " ")
			return reportTask("create", func(config *js.Object) (taskOutcome, error) {
				root, err := targetWorkspace(workspaceFlag(c))
				if err != nil {
					return taskOutcome{}, err
				}
				m, _, err := openWorkspace(root)
				if err != nil {
					return taskOutcome{}, err
				}
				parsed, err := parseAddInput(input, now(), cfgDateFormat(config))
				if err != nil {
					return taskOutcome{}, err
				}
				boards := boardRepository(m)
				boardID := mdwal.INBOX
				if parsed.board != nil {
					if boardID, err = resolveBoard(boards, root, *parsed.board); err != nil {
						return taskOutcome{}, err
					}
				}
				if _, err := taskRepository(m).create(js.Obj("title", parsed.title, "rrule", nullable(parsed.rrule), "board", boardID)); err != nil {
					return taskOutcome{}, err
				}
				outcome, err := datedOutcome(parsed.title, "created", parsed.rrule, config)
				if err != nil {
					return taskOutcome{}, err
				}
				if boardID != mdwal.INBOX {
					name := *parsed.board
					if b := boards.findByID(boardID); b != nil {
						name = str(b, "name")
					}
					outcome.value = &valueChip{" in ", name}
				}
				return outcome, nil
			})
		}}
}

func listCommand() *Command {
	return &Command{Name: "list", Description: "list tasks",
		Options: []Option{
			flag("global", "list across every workspace"),
			flag("checked", "include completed tasks"),
			flag("deleted", "show the bin instead of the live tasks"),
			flag("starred", "only starred tasks"),
			flag("hasDescription", "only tasks that carry a description"),
			{Name: "period", Description: "only tasks due within a window, e.g. 2d, 3m, 1y", Kind: String},
			{Name: "in-board", Description: "only tasks on a board (name, number or inbox)", Kind: String},
			workspaceOption(),
		},
		Handler: func(c *Ctx) error {
			f := taskFilters{checked: c.Bool("checked"), deleted: c.Bool("deleted"), starred: c.Bool("starred"), hasDescription: c.Bool("hasDescription")}
			if p, ok := c.Str("period"); ok {
				f.period = &p
			}
			if b, ok := c.Str("in-board"); ok {
				f.inBoard = &b
			}
			global := c.Bool("global")
			var names []string
			if global {
				all, err := allWorkspaces()
				if err != nil {
					return err
				}
				names = all
			} else {
				name := workspaceFlag(c)
				if name == "" {
					current, err := currentWorkspaceName()
					if err != nil {
						return err
					}
					name = current
				}
				names = []string{name}
			}
			var store kvStore
			if global {
				store = globalKvStore(dataHome())
			} else {
				store = workspaceKvStore(workspacePath(names[0]), "tasks")
			}
			if err := store.reset(); err != nil {
				return err
			}
			// Every numbering this listing replaces is cleared too.
			if global {
				for _, n := range names {
					if err := workspaceKvStore(workspacePath(n), "tasks").reset(); err != nil {
						return err
					}
				}
			} else if err := globalKvStore(dataHome()).reset(); err != nil {
				return err
			}
			var groups []taskGroup
			for _, n := range names {
				g, err := gatherTaskGroups(n, f, store, global)
				if err != nil {
					return err
				}
				groups = append(groups, g...)
			}
			config, err := readConfig()
			if err != nil {
				return err
			}
			return present.taskList(groups, now(), config)
		}}
}

// editTask hands a `set` field's body the whole tail as its value.
func editTask(positional []string, workspace string, edit func(value string, task *js.Object, tasks repository, m *mdwal.Mdwal, root string, config *js.Object) (taskOutcome, error)) error {
	reference := first(positional)
	var rest []string
	if len(positional) > 1 {
		rest = positional[1:]
	}
	return onTask("update", reference, workspace, func(task *js.Object, tasks repository, m *mdwal.Mdwal, root string, config *js.Object) (taskOutcome, error) {
		return edit(strings.Join(rest, " "), task, tasks, m, root, config)
	})
}

var taskFields = []string{"title", "description", "date-time", "board"}

func setCommand() *Command {
	field := func(name, description string, edit func(string, *js.Object, repository, *mdwal.Mdwal, string, *js.Object) (taskOutcome, error)) *Command {
		return &Command{Name: name, Description: description, Options: []Option{workspaceOption()},
			Handler: func(c *Ctx) error { return editTask(c.Positional, workspaceFlag(c), edit) }}
	}
	return &Command{Name: "set", Description: "edit a task field",
		Handler: rejectUnknownSubcommand(func(f string) string {
			return fmt.Sprintf("a task has no %s — try %s", f, strings.Join(taskFields, ", "))
		}),
		Commands: []*Command{
			field("title", "retitle a task", func(value string, task *js.Object, tasks repository, _ *mdwal.Mdwal, _ string, _ *js.Object) (taskOutcome, error) {
				if _, err := tasks.update(with(task, "title", value)); err != nil {
					return taskOutcome{}, err
				}
				return taskOutcome{title: value, predicate: "title updated"}, nil
			}),
			field("description", "set a task's description", func(value string, task *js.Object, tasks repository, _ *mdwal.Mdwal, _ string, _ *js.Object) (taskOutcome, error) {
				if _, err := tasks.update(with(task, "description", value)); err != nil {
					return taskOutcome{}, err
				}
				return taskOutcome{title: str(task, "title"), predicate: "description updated"}, nil
			}),
			field("date-time", "schedule a task, or clear its schedule with an empty value", func(value string, task *js.Object, tasks repository, _ *mdwal.Mdwal, _ string, config *js.Object) (taskOutcome, error) {
				if value == "" {
					if _, err := tasks.update(with(task, "rrule", nil)); err != nil {
						return taskOutcome{}, err
					}
					return taskOutcome{title: str(task, "title"), predicate: "schedule removed"}, nil
				}
				date, err := parseDate(value, cfgDateFormat(config))
				if err != nil {
					return taskOutcome{}, err
				}
				rule, err := rrule.WithDtstart(strPtr(task, "rrule"), date)
				if err != nil {
					return taskOutcome{}, err
				}
				if _, err := tasks.update(with(task, "rrule", rule)); err != nil {
					return taskOutcome{}, err
				}
				return datedOutcome(str(task, "title"), "schedule updated", &rule, config)
			}),
			field("board", "move a task to a board (name, number or inbox)", func(value string, task *js.Object, tasks repository, m *mdwal.Mdwal, root string, _ *js.Object) (taskOutcome, error) {
				boards := boardRepository(m)
				boardID := mdwal.INBOX
				if value != "" {
					id, err := resolveBoard(boards, root, value)
					if err != nil {
						return taskOutcome{}, err
					}
					boardID = id
				}
				if _, err := tasks.update(with(task, "board", boardID)); err != nil {
					return taskOutcome{}, err
				}
				name := "inbox"
				if boardID != mdwal.INBOX {
					name = value
					if b := boards.findByID(boardID); b != nil {
						name = str(b, "name")
					}
				}
				return taskOutcome{title: str(task, "title"), predicate: "moved", value: &valueChip{" to ", name}}, nil
			}),
		}}
}

// ── boards ───────────────────────────────────────────────────────────────────

func onBoard(action, reference, workspace string, edit func(board *js.Object, boards repository, m *mdwal.Mdwal, root string) (entityOutcome, error)) error {
	return reportBoard(action, func(*js.Object) (entityOutcome, error) {
		root, err := targetWorkspace(workspace)
		if err != nil {
			return entityOutcome{}, err
		}
		m, _, err := openWorkspace(root)
		if err != nil {
			return entityOutcome{}, err
		}
		boards := boardRepository(m)
		id, err := resolveBoard(boards, root, reference)
		if err != nil {
			return entityOutcome{}, err
		}
		board := boards.findByID(id)
		if board == nil {
			return entityOutcome{}, fmt.Errorf("no such board: %s", reference)
		}
		return edit(board, boards, m, root)
	})
}

func listBoards(workspace string) error {
	root, err := targetWorkspace(workspace)
	if err != nil {
		return err
	}
	m, _, err := openWorkspace(root)
	if err != nil {
		return err
	}
	store := workspaceKvStore(root, "boards")
	if err := store.reset(); err != nil {
		return err
	}
	live, err := boardRepository(m).filter(func(b *js.Object) bool { return !boolean(b, "deleted") })
	if err != nil {
		return err
	}
	var rows []boardRow
	for _, b := range live {
		n, err := store.set(str(b, "id"))
		if err != nil {
			return err
		}
		rows = append(rows, boardRow{n, boardDisplay{str(b, "id"), str(b, "name"), str(b, "icon"), str(b, "color")}})
	}
	config, err := readConfig()
	if err != nil {
		return err
	}
	return present.boardList(rows, config)
}

// deleteBoardTasks soft-deletes the board's live tasks, returning how many.
func deleteBoardTasks(tasks repository, boardID string) (int, error) {
	live, err := tasks.filter(func(t *js.Object) bool { return str(t, "board") == boardID && !boolean(t, "deleted") })
	if err != nil {
		return 0, err
	}
	for _, t := range live {
		if _, err := tasks.setDeleted(str(t, "id"), true); err != nil {
			return 0, err
		}
	}
	return len(live), nil
}

var boardFieldNames = []string{"name", "icon", "color"}

func setBoardField(field, action, predicate, connector string) *Command {
	return &Command{Name: field, Description: action, Options: []Option{workspaceOption()},
		Handler: func(c *Ctx) error {
			reference := first(c.Positional)
			var rest []string
			if len(c.Positional) > 1 {
				rest = c.Positional[1:]
			}
			return onBoard(action, reference, workspaceFlag(c), func(board *js.Object, boards repository, _ *mdwal.Mdwal, _ string) (entityOutcome, error) {
				next := strings.Join(rest, " ")
				if _, err := boards.update(with(board, field, next)); err != nil {
					return entityOutcome{}, err
				}
				return entityOutcome{name: str(board, "name"), predicate: predicate, value: &valueChip{connector, next}}, nil
			})
		}}
}

func boardCommand() *Command {
	return &Command{Name: "board", Description: "create, list and edit boards",
		Handler: rejectUnknownSubcommand(func(v string) string { return "unknown board command: " + v }),
		Commands: []*Command{
			{Name: "add", Description: "create a board",
				Options: []Option{{Name: "icon", Description: "the board's icon", Kind: StringDflt}, workspaceOption()},
				Handler: func(c *Ctx) error {
					name := strings.Join(c.Positional, " ")
					icon, _ := c.Str("icon")
					return reportBoard("create this board", func(*js.Object) (entityOutcome, error) {
						root, err := targetWorkspace(workspaceFlag(c))
						if err != nil {
							return entityOutcome{}, err
						}
						m, _, err := openWorkspace(root)
						if err != nil {
							return entityOutcome{}, err
						}
						if _, err := boardRepository(m).create(js.Obj("name", name, "icon", icon)); err != nil {
							return entityOutcome{}, err
						}
						return entityOutcome{name: name, predicate: "created"}, nil
					})
				}},
			{Name: "list", Description: "list boards", Options: []Option{workspaceOption()},
				Handler: func(c *Ctx) error { return listBoards(workspaceFlag(c)) }},
			{Name: "set", Description: "edit a board field",
				Handler: rejectUnknownSubcommand(func(f string) string {
					return fmt.Sprintf("a board has no %s — try %s", f, strings.Join(boardFieldNames, ", "))
				}),
				Commands: []*Command{
					setBoardField("name", "rename this board", "renamed", " to "),
					setBoardField("icon", "update this board's icon", "icon updated", ": "),
					setBoardField("color", "update this board's color", "color updated", ": "),
				}},
			{Name: "delete", Description: "soft-delete a board, moving its tasks to the inbox",
				Options: []Option{flag("tasks", "soft-delete the board's tasks instead of moving them to the inbox"), workspaceOption()},
				Handler: func(c *Ctx) error {
					return eachReference(c.Positional, func(ref string) error {
						return onBoard("delete this board", ref, workspaceFlag(c), func(board *js.Object, boards repository, m *mdwal.Mdwal, _ string) (entityOutcome, error) {
							tasks := taskRepository(m)
							id := str(board, "id")
							if c.Bool("tasks") {
								n, err := deleteBoardTasks(tasks, id)
								if err != nil {
									return entityOutcome{}, err
								}
								if _, err := boards.setDeleted(id, true); err != nil {
									return entityOutcome{}, err
								}
								if n > 0 {
									suffix := ". its tasks were deleted"
									return entityOutcome{name: str(board, "name"), predicate: "deleted", suffix: &suffix}, nil
								}
								return entityOutcome{name: str(board, "name"), predicate: "deleted"}, nil
							}
							orphaned, err := tasks.filter(func(t *js.Object) bool { return str(t, "board") == id })
							if err != nil {
								return entityOutcome{}, err
							}
							for _, t := range orphaned {
								if _, err := tasks.update(with(t, "board", mdwal.INBOX)); err != nil {
									return entityOutcome{}, err
								}
							}
							if _, err := boards.setDeleted(id, true); err != nil {
								return entityOutcome{}, err
							}
							if len(orphaned) > 0 {
								suffix := ". its tasks were moved to inbox"
								return entityOutcome{name: str(board, "name"), predicate: "deleted", suffix: &suffix}, nil
							}
							return entityOutcome{name: str(board, "name"), predicate: "deleted"}, nil
						})
					})
				}},
			{Name: "clear", Description: "soft-delete every task of a board, keeping the board", Options: []Option{workspaceOption()},
				Handler: func(c *Ctx) error {
					return eachReference(c.Positional, func(ref string) error {
						return onBoard("clear this board", ref, workspaceFlag(c), func(board *js.Object, _ repository, m *mdwal.Mdwal, _ string) (entityOutcome, error) {
							n, err := deleteBoardTasks(taskRepository(m), str(board, "id"))
							if err != nil {
								return entityOutcome{}, err
							}
							suffix := fmt.Sprintf(". %d tasks deleted", n)
							return entityOutcome{name: str(board, "name"), predicate: "cleared", suffix: &suffix}, nil
						})
					})
				}},
			{Name: "recover", Description: "restore a deleted board", Options: []Option{workspaceOption()},
				Handler: func(c *Ctx) error {
					return eachReference(c.Positional, func(ref string) error {
						return onBoard("recover this board", ref, workspaceFlag(c), func(board *js.Object, boards repository, _ *mdwal.Mdwal, _ string) (entityOutcome, error) {
							done, err := boards.setDeleted(str(board, "id"), false)
							if err != nil {
								return entityOutcome{}, err
							}
							if !done {
								return entityOutcome{}, fmt.Errorf("no such board: %s", ref)
							}
							return entityOutcome{name: str(board, "name"), predicate: "recovered"}, nil
						})
					})
				}},
		}}
}

// ── workspaces ───────────────────────────────────────────────────────────────

func listWorkspaces() error {
	current, err := currentWorkspaceName()
	if err != nil {
		return err
	}
	all, err := allWorkspaces()
	if err != nil {
		return err
	}
	config, err := readConfig()
	if err != nil {
		return err
	}
	return present.workspaceList(all, current, config)
}

func useWorkspace(name string) error {
	return reportWorkspace("switch workspace", func(*js.Object) (entityOutcome, error) {
		if err := assertWorkspaceExists(name); err != nil {
			return entityOutcome{}, err
		}
		if err := setCurrentWorkspace(name); err != nil {
			return entityOutcome{}, err
		}
		return entityOutcome{name: name, predicate: "now in use"}, nil
	})
}

func workspaceCommand() *Command {
	confirm := func(description string) Option { return flag("confirm", description) }
	return &Command{Name: "workspace", Description: "create, switch and remove workspaces",
		Handler: rejectUnknownSubcommand(func(v string) string { return "unknown workspace command: " + v }),
		Commands: []*Command{
			{Name: "list", Description: "list workspaces, marking the current one", Handler: func(*Ctx) error { return listWorkspaces() }},
			{Name: "create", Description: "create a workspace", Handler: func(c *Ctx) error {
				name := first(c.Positional)
				return reportWorkspace("create this workspace", func(*js.Object) (entityOutcome, error) {
					if err := nodefs.MkdirAll(workspacePath(name)); err != nil {
						return entityOutcome{}, err
					}
					return entityOutcome{name: name, predicate: "created"}, nil
				})
			}},
			{Name: "use", Description: "switch the current workspace", Handler: func(c *Ctx) error { return useWorkspace(first(c.Positional)) }},
			{Name: "rename", Description: "rename a workspace", Handler: func(c *Ctx) error {
				name := first(c.Positional)
				second := ""
				if len(c.Positional) > 1 {
					second = c.Positional[1]
				}
				return reportWorkspace("rename this workspace", func(*js.Object) (entityOutcome, error) {
					if err := assertWorkspaceExists(name); err != nil {
						return entityOutcome{}, err
					}
					if err := nodefs.Rename(workspacePath(name), workspacePath(second)); err != nil {
						return entityOutcome{}, err
					}
					current, err := currentWorkspaceName()
					if err != nil {
						return entityOutcome{}, err
					}
					if current == name {
						if err := setCurrentWorkspace(second); err != nil {
							return entityOutcome{}, err
						}
					}
					return entityOutcome{name: name, predicate: "renamed", value: &valueChip{" to ", second}}, nil
				})
			}},
			{Name: "delete", Description: "delete a workspace's local folder",
				Options: []Option{confirm("confirm that the local folder is removed")},
				Handler: func(c *Ctx) error {
					name := first(c.Positional)
					return reportWorkspace("delete this workspace", func(*js.Object) (entityOutcome, error) {
						if err := assertWorkspaceExists(name); err != nil {
							return entityOutcome{}, err
						}
						current, err := currentWorkspaceName()
						if err != nil {
							return entityOutcome{}, err
						}
						if current == name {
							return entityOutcome{}, fmt.Errorf("%s is the current workspace — `taskthing use <other>` before deleting it", name)
						}
						if !c.Bool("confirm") {
							return entityOutcome{}, fmt.Errorf("deleting %s removes its local folder — pass --confirm", name)
						}
						if err := nodefs.RemoveAll(workspacePath(name)); err != nil {
							return entityOutcome{}, err
						}
						return entityOutcome{name: name, predicate: "deleted"}, nil
					})
				}},
			{Name: "remote", Description: "attach or detach this workspace's git remote",
				Handler: rejectUnknownSubcommand(func(v string) string { return "unknown remote command: " + v }),
				Commands: []*Command{
					{Name: "add", Description: "attach a git remote by URL",
						Options: []Option{confirm("confirm destroying local work to join the remote"), workspaceOption()},
						Handler: func(c *Ctx) error {
							return reportWorkspace("add this remote", func(*js.Object) (entityOutcome, error) {
								if len(c.Positional) == 0 {
									return entityOutcome{}, errors.New("remote add needs a URL")
								}
								url := c.Positional[0]
								name, err := targetWorkspaceName(workspaceFlag(c))
								if err != nil {
									return entityOutcome{}, err
								}
								_, mgr, err := openWorkspace(workspacePath(name))
								if err != nil {
									return entityOutcome{}, err
								}
								if err := mgr.init(url, c.Bool("confirm")); err != nil {
									return entityOutcome{}, err
								}
								return entityOutcome{name: name, predicate: "remote added", value: &valueChip{": ", url}}, nil
							})
						}},
					{Name: "remove", Description: "detach this workspace's git remote", Options: []Option{workspaceOption()},
						Handler: func(c *Ctx) error {
							return reportWorkspace("remove this remote", func(*js.Object) (entityOutcome, error) {
								name, err := targetWorkspaceName(workspaceFlag(c))
								if err != nil {
									return entityOutcome{}, err
								}
								_, mgr, err := openWorkspace(workspacePath(name))
								if err != nil {
									return entityOutcome{}, err
								}
								if err := mgr.disassociate(); err != nil {
									return entityOutcome{}, err
								}
								return entityOutcome{name: name, predicate: "remote removed"}, nil
							})
						}},
				}},
		}}
}

// ── sync verbs ───────────────────────────────────────────────────────────────

func runSyncing(workspace string, plan func(name string, m *manager) (commandCopy, func() error)) error {
	name, err := targetWorkspaceName(workspace)
	if err != nil {
		return err
	}
	_, mgr, err := openWorkspace(workspacePath(name))
	if err != nil {
		return err
	}
	config, err := readConfig()
	if err != nil {
		return err
	}
	copy, op := plan(name, mgr)
	return present.progress(copy, op, config)
}

func syncVerb(name, description string, copy func(string) commandCopy, op func(*manager) error) *Command {
	return &Command{Name: name, Description: description, Options: []Option{workspaceOption()},
		Handler: func(c *Ctx) error {
			return runSyncing(workspaceFlag(c), func(ws string, m *manager) (commandCopy, func() error) {
				return copy(ws), func() error { return op(m) }
			})
		}}
}

func truncateCommand() *Command {
	return &Command{Name: "truncate", Description: "bound a branch's history to its most recent commits",
		Options: []Option{
			{Name: "keep", Description: "the number of commits to keep", Kind: PositiveInt},
			{Name: "branch", Description: "the branch to truncate (default: this peer's own)", Kind: String},
			workspaceOption(),
		},
		Handler: func(c *Ctx) error {
			var branch *string
			if b, ok := c.Str("branch"); ok {
				branch = &b
			}
			keep, _ := c.Flags["keep"].(int)
			return runSyncing(workspaceFlag(c), func(ws string, m *manager) (commandCopy, func() error) {
				label := ""
				if branch != nil {
					label = *branch
				}
				return truncateCopy(ws, label), func() error { return m.truncateHistory(branch, keep) }
			})
		}}
}

// ── config, theme, update, migrations, install, entity ───────────────────────

var configKeys = []string{"dateFormat", "nerdfont", "theme", "autoUpdate", "autoSync", "autoRebuild", "autoTruncate"}

func configCommand() *Command {
	return &Command{Name: "config", Description: "open settings (piped: print them)",
		Handler: func(*Ctx) error {
			current, err := readConfig()
			if err != nil {
				return err
			}
			if !stdoutTTY {
				for _, k := range current.Keys() {
					consoleLog(k + " " + jsStringOf(current.Value(k)))
				}
				return nil
			}
			t, err := themeOf(current)
			if err != nil {
				return err
			}
			update, err := runConfigScreen(t)
			if err != nil {
				return err
			}
			if update.key == "" {
				return nil
			}
			return reportConfig(current, update.key, func() error {
				parsed, err := mdwal.ConfigSchema.Parse(with(current, update.key, update.value))
				if err != nil {
					return err
				}
				return writeConfig(parsed)
			})
		},
		Commands: []*Command{{Name: "set", Description: "set a setting non-interactively", Handler: func(c *Ctx) error {
			key := first(c.Positional)
			value := ""
			if len(c.Positional) > 1 {
				value = strings.Join(c.Positional[1:], " ")
			}
			if key == "currentWorkspace" {
				return errors.New("the current workspace changes with `taskthing use <name>`")
			}
			if !slices.Contains(configKeys, key) {
				if len(c.Positional) == 0 {
					key = "undefined"
				}
				return fmt.Errorf("no such setting: %s — try %s", key, strings.Join(configKeys, ", "))
			}
			var coerced any = value
			switch key {
			case "nerdfont":
				coerced = value == "true"
			case "autoSync", "autoRebuild", "autoTruncate":
				coerced = js.ToNumber(value)
			}
			current, err := readConfig()
			if err != nil {
				return err
			}
			parsed, err := mdwal.ConfigSchema.Parse(with(current, key, coerced))
			if err != nil {
				return err
			}
			return writeConfig(parsed)
		}}},
	}
}

func themeCommand() *Command {
	return &Command{Name: "theme", Description: "open the theme editor", Handler: func(*Ctx) error {
		current, err := readConfig()
		if err != nil {
			return err
		}
		if !stdoutTTY {
			return errors.New("`theme` needs an interactive terminal")
		}
		t, err := themeOf(current)
		if err != nil {
			return err
		}
		value, err := runThemeScreen(t)
		if err != nil {
			return err
		}
		return reportConfig(current, "theme", func() error {
			parsed, err := mdwal.ConfigSchema.Parse(with(current, "theme", value))
			if err != nil {
				return err
			}
			return writeConfig(parsed)
		})
	}}
}

func updateCommand() *Command {
	force := flag("force", "check even if the cached answer is still fresh")
	check := func(force bool) error {
		u := buildUpdater()
		config, err := readConfig()
		if err != nil {
			return err
		}
		return present.progressDynamic(updateCheckPending, updateCheckFailure, func() (string, error) {
			r, err := u.check(force)
			if err != nil {
				return "", err
			}
			if !r.pending {
				return updateCheckLatest, nil
			}
			return updateCheckUpdate(r.current, r.target), nil
		}, config)
	}
	return &Command{Name: "update", Description: "self-update from GitHub", Options: []Option{force},
		Handler: func(c *Ctx) error { return check(c.Bool("force")) },
		Commands: []*Command{
			{Name: "check", Description: "report whether a newer release is available", Options: []Option{force},
				Handler: func(c *Ctx) error { return check(c.Bool("force")) }},
			{Name: "apply", Description: "download and swap in the newest release", Handler: func(*Ctx) error {
				u := buildUpdater()
				config, err := readConfig()
				if err != nil {
					return err
				}
				// Running `update apply` is itself the confirmation.
				return present.downloadProgress(updateApplyPending, updateApplyFailure, func(report func(downloadPhase)) (string, error) {
					result, target, err := u.apply(func() bool { return true },
						func(done int64, total *int64) { report(downloadPhase{downloaded: done, total: total}) },
						func() { report(downloadPhase{applying: true}) })
					if err != nil {
						return "", err
					}
					switch result {
					case upToDate:
						return updateCheckLatest, nil
					case declined:
						return "update cancelled", nil
					}
					return updateApplySuccess(target), nil
				}, config)
			}},
		}}
}

func migrationsCommand() *Command {
	return &Command{Name: "migrations", Description: "report applied and pending migrations", Options: []Option{workspaceOption()},
		Handler: func(c *Ctx) error {
			root, err := targetWorkspace(workspaceFlag(c))
			if err != nil {
				return err
			}
			order, recorded := recordedMigrations(root)
			seen := map[string]bool{}
			var versions []string
			for _, m := range Migrations {
				if !seen[m.Version] {
					seen[m.Version] = true
					versions = append(versions, m.Version)
				}
			}
			for _, v := range order {
				if !seen[v] {
					seen[v] = true
					versions = append(versions, v)
				}
			}
			slices.SortStableFunc(versions, semverOrder)
			var rows []migrationRow
			for i, v := range versions {
				rows = append(rows, migrationRow{i + 1, v, recorded[v]})
			}
			config, err := readConfig()
			if err != nil {
				return err
			}
			return present.migrationList(rows, config)
		}}
}

func installCommand() *Command {
	return &Command{Name: "install", Description: "first-run scaffold and setup",
		Options: []Option{
			flag("force", "reinstall over an existing install"),
			{Name: "date-format", Description: "date order to use when piped", Kind: Enum, Values: []string{"america", "europe"}},
			flag("nerdfont", "use nerdfont glyphs when piped"),
		},
		Handler: func(c *Ctx) error {
			nerdfont := false
			err := func() error {
				if nodefs.Exists(configPath()) && !c.Bool("force") {
					return errors.New("taskthing is already installed — pass --force to reinstall")
				}
				dateFormat := "america"
				if stdoutTTY {
					result, err := runInstallForm()
					if err != nil {
						return err
					}
					dateFormat, nerdfont = result.dateFormat, result.nerdfont
				} else {
					if f, ok := c.Str("date-format"); ok {
						dateFormat = f
					}
					nerdfont = c.Bool("nerdfont")
				}
				if err := nodefs.MkdirAll(workspacePath("local")); err != nil {
					return err
				}
				config, err := mdwal.ConfigSchema.Parse(js.Obj("currentWorkspace", "local", "dateFormat", dateFormat, "nerdfont", nerdfont, "theme", ""))
				if err != nil {
					return err
				}
				return writeConfig(config)
			}()
			if err != nil {
				config, _ := mdwal.ConfigSchema.Parse(js.Obj("currentWorkspace", "local", "nerdfont", nerdfont))
				return present.fail("something went wrong during installation. error: "+summarize(err), config)
			}
			consoleLog(createGlyphs(nerdfont).success + " taskthing installed successfully!")
			return nil
		}}
}

func entityCommand() *Command {
	logOption := flag("log", "emit the migration line (required — this command only authors)")
	op := func(name, description, usage string) *Command {
		return &Command{Name: name, Description: description, Options: []Option{logOption}, Handler: func(c *Ctx) error {
			if !c.Bool("log") {
				return errors.New("entity authors migrations: pass --log to emit the migration line")
			}
			if len(c.Positional) == 0 {
				return errors.New("usage: " + usage)
			}
			var newName *string
			if len(c.Positional) > 1 {
				newName = &c.Positional[1]
			}
			text, err := authorEntityLog(name, c.Positional[0], newName)
			if err != nil {
				return err
			}
			os.Stdout.WriteString(text)
			return nil
		}}
	}
	return &Command{Name: "entity", Description: "author a structural migration for an entity folder (plumber)",
		Handler: rejectUnknownSubcommand(func(o string) string { return "unknown entity op: " + o + " (expected create|rename|delete)" }),
		Commands: []*Command{
			op("create", "author a folder creation", "taskthing entity create <name> --log"),
			op("rename", "author a folder rename", "taskthing entity rename <name> <new-name> --log"),
			op("delete", "author a folder removal", "taskthing entity delete <name> --log"),
		}}
}
