package policy

// This table is the fail-closed contract of the guard. It was taken from live
// tools/list output, not from the servers' READMEs:
//
//   - @modelcontextprotocol/server-filesystem@0.2.0 — 14 tools. The version is
//     pinned in agent.config.yaml precisely because this table is tied to it.
//     Note read_file: an undocumented legacy alias that would slip past an
//     allow-list built from the README. Hence: anything from a file server that
//     is missing here is denied.
//   - fff-mcp 0.10.0 — 3 tools, none of which takes a path argument (the search
//     scope is fixed at process start), so they are neutral for path checks.
//   - ../git-mcp-server — 3 tools. Neutral as well, except git_diff's optional
//     path argument, which is checked like a file path when present.
//
// Every tool that prints file paths (search results, git listings and diffs)
// has OutputPaths set: its result passes through FS.FilterOutput, so a deny
// glob cannot be side-stepped by searching for a secret instead of reading it.

// FSToolNames lists every @modelcontextprotocol/server-filesystem@0.2.0 tool.
func FSToolNames() []string {
	return []string{
		"read_file",
		"read_text_file",
		"read_media_file",
		"read_multiple_files",
		"write_file",
		"edit_file",
		"create_directory",
		"list_directory",
		"list_directory_with_sizes",
		"directory_tree",
		"move_file",
		"search_files",
		"get_file_info",
		"list_allowed_directories",
	}
}

// SearchToolNames lists every fff-mcp 0.10.0 tool.
func SearchToolNames() []string {
	return []string{
		"find_files",
		"grep",
		"multi_grep",
	}
}

// GitToolNames lists every ../git-mcp-server tool.
func GitToolNames() []string {
	return []string{
		"git_current_branch",
		"git_list_files",
		"git_diff",
	}
}

// LocalToolNames lists the tools of the built-in "local" server
// (internal/adapter/localtools). The file tools reuse the server-filesystem
// names and specs, so the same guard applies unchanged.
func LocalToolNames() []string {
	return []string{
		"read_text_file",
		"read_multiple_files",
		"list_directory",
		"directory_tree",
		"search_files",
		"get_file_info",
		"write_file",
		"edit_file",
		"create_directory",
		"grep",
		"run_command",
	}
}

// DefaultSpecs returns the fail-closed tool table.
func DefaultSpecs() map[string]ToolSpec {
	return map[string]ToolSpec{
		// --- server-filesystem: reads ---
		"read_file":                 {Kind: KindRead, PathArgs: []string{"path"}, MustExist: true},
		"read_text_file":            {Kind: KindRead, PathArgs: []string{"path"}, MustExist: true},
		"read_media_file":           {Kind: KindRead, PathArgs: []string{"path"}, MustExist: true},
		"read_multiple_files":       {Kind: KindRead, ListArgs: []string{"paths"}, MustExist: true},
		"list_directory":            {Kind: KindRead, PathArgs: []string{"path"}, MustExist: true},
		"list_directory_with_sizes": {Kind: KindRead, PathArgs: []string{"path"}, MustExist: true},
		"directory_tree":            {Kind: KindRead, PathArgs: []string{"path"}, MustExist: true},
		"search_files":              {Kind: KindRead, PathArgs: []string{"path"}, MustExist: true, OutputPaths: true},
		"get_file_info":             {Kind: KindRead, PathArgs: []string{"path"}, MustExist: true},

		// --- server-filesystem: writes ---
		"write_file":       {Kind: KindWrite, PathArgs: []string{"path"}, MustExist: false},
		"create_directory": {Kind: KindWrite, PathArgs: []string{"path"}, MustExist: false, CreatesDir: true},
		"edit_file":        {Kind: KindWrite, PathArgs: []string{"path"}, MustExist: true},
		// move_file: source must exist, destination must not — NewPathArgs
		// expresses the split without special-casing the tool name.
		"move_file": {
			Kind:        KindWrite,
			PathArgs:    []string{"source", "destination"},
			MustExist:   true,
			NewPathArgs: []string{"destination"},
		},

		// --- server-filesystem: neutral ---
		"list_allowed_directories": {Kind: KindNeutral},

		// --- fff-mcp: neutral, no path arguments at all; output carries paths ---
		"find_files": {Kind: KindNeutral, OutputPaths: true},
		"grep":       {Kind: KindNeutral, OutputPaths: true},
		"multi_grep": {Kind: KindNeutral, OutputPaths: true},

		// --- git-mcp-server: neutral; listings and diffs carry paths ---
		"git_current_branch": {Kind: KindNeutral},
		"git_list_files":     {Kind: KindNeutral, OutputPaths: true},
		"git_diff":           {Kind: KindNeutral, OutputPaths: true, OptionalPathArgs: []string{"path"}},

		// --- built-in "local" server: run_command (argv, no shell) ---
		"run_command": {Kind: KindWrite, Exec: true},
	}
}

// DefaultDenyGlobs returns the default deny list. It is applied to reads and
// writes alike, after the path has been resolved (so a symlink cannot dodge it).
func DefaultDenyGlobs() []string {
	return []string{
		".git/**",
		".env*",
		"**/*.pem",
		"**/*.key",
		"id_rsa*",
		".ssh/**",
		"agent.config.yaml",
		"agent.review.yaml",
		"*.db",
		"**/node_modules/**",
		// Nested repositories and key stores, not only the ones at the root.
		"**/.git/**",
		"**/.ssh/**",
		"**/.env*",
		// The agent's own state: dialogue history, loop state and logs.
		"**/chat_history*.json",
		"**/*.loop*.json",
		"**/*.loop.lock",
		"**/agent-tui.log",
	}
}
