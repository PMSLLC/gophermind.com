package tui

// slashCommand describes a top-level slash command for help text and (later)
// completion. Arg is a short placeholder shown after the name when the
// command takes an argument, e.g. "<name>"; it is "" for argument-less
// commands. This registry is metadata only — dispatch logic in handleSubmit
// is unaffected by it.
//
// "/generate" is intentionally excluded: it is a /project sub-mode, not a
// top-level command (see project.go). "/phase" sub-commands have their own
// help string, phaseSlashHelp (commands.go), which is not folded in here.
type slashCommand struct {
	Name string
	Arg  string
	Desc string
}

var slashCommands = []slashCommand{
	{Name: "/help", Arg: "", Desc: "show this help"},
	{Name: "/clear", Arg: "", Desc: "clear the transcript and reset the session"},
	{Name: "/project", Arg: "<name>", Desc: "start the guided new-project flow"},
	{Name: "/phase", Arg: "<cmd>", Desc: "run a PhaseFlow workflow command"},
	{Name: "/config", Arg: "", Desc: "open the configuration wizard"},
	{Name: "/temp", Arg: "<0-2>", Desc: "set sampling temperature"},
	{Name: "/topp", Arg: "<0-1>", Desc: "set top-p sampling"},
	{Name: "/goal", Arg: "<text>", Desc: "set a persistent steering goal (\"/goal\" shows, \"/goal clear\" removes)"},
	{Name: "/exit", Arg: "", Desc: "quit gophermind"},
	{Name: "/quit", Arg: "", Desc: "quit gophermind (alias of /exit)"},
}

// commandNames returns the registered command names in registry order.
func commandNames() []string {
	names := make([]string, len(slashCommands))
	for i, c := range slashCommands {
		names[i] = c.Name
	}
	return names
}

// helpLine renders the registry into the "/help" transcript line.
func helpLine() string {
	s := "Commands: "
	for i, c := range slashCommands {
		if i > 0 {
			s += "  "
		}
		s += c.Name
		if c.Arg != "" {
			s += " " + c.Arg
		}
	}
	s += " · y/n/a to approve · Esc to interrupt"
	return s
}
