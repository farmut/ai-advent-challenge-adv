-- Local LLM code assistant (source of truth: ai-advent-challenge-adv/Day4/nvim/ai.lua,
-- symlinked into ~/.config/nvim/lua/plugins/ai.lua by Day4/nvim/install.sh):
--   minuet-ai.nvim     -> FIM autocompletion through blink.cmp
--   codecompanion.nvim -> chat / inline edits / agent (@{agent})
--
-- Project rules: the nearest `.ai/system-prompt.md` above cwd (stops at $HOME) is appended
-- to the system prompt of chat, inline and @{agent}. Rule files are read as text only.
-- The global ~/.claude/CLAUDE.md and other preset rule files are NOT sent to the local model.
--
-- Switch `backend` / `chat_model` and restart nvim. `:AILoad` loads the chat model into
-- LM Studio with the context length from `models` below, so both sides agree on the window.

---@type "ollama"|"lmstudio"
local backend = "lmstudio"

-- Chat/agent model for LM Studio, a key of `models`
local chat_model = vim.env.AI_CHAT_MODEL or "qwen/qwen3-coder-30b"

local request_timeout = 3 -- seconds, blink timeout_ms must match

-- Generation presets per chat model. context_window must match `lms load -c`
-- (`:AILoad` uses it); codecompanion uses it for context editing/compaction.
local models = {
  ["qwen/qwen3-coder-30b"] = {
    formatted_name = "Qwen3 Coder 30B-A3B",
    context_window = 32768,
    temperature = 0.2,
    top_p = 0.9,
    top_k = 20,
    max_tokens = 8192,
  },
  ["qwen/qwen3.8-27b"] = {
    formatted_name = "Qwen3.8 27B",
    context_window = 32768,
    temperature = 0.2,
    top_p = 0.9,
    top_k = 20,
    max_tokens = 8192,
    -- Thinking at T=0.2 loops for 8k+ tokens at ~13 tok/s (Day4 REPORT); without it the agent run passed
    reasoning_effort = "none",
  },
}

-- Harness overrides (Day4/harness/run.sh), unset in normal use
local function env_number(name)
  local v = vim.env[name]
  return v and tonumber(v) or nil
end

local preset = vim.deepcopy(models[chat_model] or models["qwen/qwen3-coder-30b"])
preset.context_window = env_number("AI_CONTEXT_WINDOW") or preset.context_window
preset.temperature = env_number("AI_TEMPERATURE") or preset.temperature
preset.top_p = env_number("AI_TOP_P") or preset.top_p
preset.max_tokens = env_number("AI_MAX_TOKENS") or preset.max_tokens
if vim.env.AI_REASONING and vim.env.AI_REASONING ~= "" then
  preset.reasoning_effort = vim.env.AI_REASONING ~= "default" and vim.env.AI_REASONING or nil
end
models[chat_model] = vim.tbl_extend("force", models[chat_model] or {}, preset)

-- Qwen2.5-Coder FIM tokens; other models (Codestral, DeepSeek-Coder) need their own
local function qwen_fim_prompt(before, after, _)
  return "<|fim_prefix|>" .. before .. "<|fim_suffix|>" .. after .. "<|fim_middle|>"
end

local backends = {
  -- `ollama pull qwen2.5-coder:1.5b-base`, `ollama pull qwen2.5-coder:7b`
  ollama = {
    name = "Ollama",
    url = "http://127.0.0.1:11434",
    fim_model = "qwen2.5-coder:1.5b-base",
    chat_model = "qwen2.5-coder:7b",
    fim_template = nil, -- Ollama supports `suffix`, minuet default template works
    adapter = "ollama",
  },
  -- `lms server start`; model ids: `curl -s http://127.0.0.1:1234/v1/models`
  lmstudio = {
    name = "LM Studio",
    url = vim.env.AI_BASE_URL or "http://127.0.0.1:1234", -- harness points it at its logging proxy
    fim_model = "qwen2.5-coder-1.5b",
    chat_model = chat_model,
    fim_template = { prompt = qwen_fim_prompt, suffix = false }, -- /v1/completions ignores `suffix`
    adapter = "lmstudio",
  },
}

local cfg = backends[backend]
if not cfg then
  vim.notify(("ai.lua: unknown backend %q, falling back to ollama"):format(backend), vim.log.levels.WARN)
  cfg = backends.ollama
end

local RULES_FILE = ".ai/system-prompt.md"
-- Rules above this share of the context window leave too little room for code and tool output
local RULES_MAX_SHARE = 0.4

---The project rules as a system prompt section, or "" when the project has none
---@return string
local function project_rules()
  local path = vim.fs.find(RULES_FILE, {
    upward = true,
    type = "file",
    path = vim.fn.getcwd(),
    stop = vim.uv.os_homedir(),
  })[1]
  if not path then return "" end

  local fd = io.open(path, "r")
  if not fd then return "" end
  local text = fd:read("*a")
  fd:close()

  local ok, tokens = pcall(function() return require("codecompanion.utils.tokens").calculate(text) end)
  if ok and tokens > preset.context_window * RULES_MAX_SHARE then
    vim.notify(
      ("ai.lua: %s is ~%d tokens, over %d%% of the %d context window"):format(
        path,
        tokens,
        RULES_MAX_SHARE * 100,
        preset.context_window
      ),
      vim.log.levels.WARN
    )
  end

  return ("\n<project_rules source=%q>\nThe rules below are mandatory for every answer, file and command in this project.\n\n%s\n</project_rules>\n"):format(
    vim.fn.fnamemodify(path, ":~:."),
    text
  )
end

-- Same shape as codecompanion's default `agent` group prompt, shortened for local models
local function agent_system_prompt(_, ctx)
  return ([[<instructions>
You are an autonomous coding agent working inside the user's project in Neovim.
Use the tools to read code, edit files and run commands until the task is fully done; do not stop at a plan.
Gather context with the tools first; do not guess file contents or command output.
After every change run the project's build and tests with the command tool and fix what fails.
Edit existing files with the edit tool and create new ones with the create tool; never print whole files instead.
Nobody can answer questions during the task: make reasonable assumptions and list them in the final answer.
Do not repeat yourself after a tool call; continue from where you left off.
</instructions>
<toolUseInstructions>
Follow each tool's JSON schema exactly and include all required properties.
Use paths relative to the working directory.
</toolUseInstructions>
<additionalContext>
All non-code text responses must be written in the %s language.
The working directory is %s. The current date is %s. The OS is %s.
</additionalContext>
%s]]):format(ctx.language, ctx.cwd, ctx.date, ctx.os, project_rules())
end

-- Schema entries share codecompanion's `parameters` mapping, so they go straight into the request body
local function generation_schema()
  local choices = {}
  for id, m in pairs(models) do
    choices[id] = {
      formatted_name = m.formatted_name,
      meta = { context_window = m.context_window },
      opts = { can_use_tools = true, has_vision = false },
    }
  end

  local function from_preset(field)
    return function(self)
      local m = models[self.schema.model.default] or preset
      return m[field]
    end
  end

  return {
    model = { default = chat_model, choices = choices },
    temperature = { order = 2, mapping = "parameters", type = "number", optional = true, default = from_preset("temperature") },
    top_p = { order = 3, mapping = "parameters", type = "number", optional = true, default = from_preset("top_p") },
    top_k = { order = 4, mapping = "parameters", type = "integer", optional = true, default = from_preset("top_k") },
    max_tokens = { order = 5, mapping = "parameters", type = "integer", optional = true, default = from_preset("max_tokens") },
    -- LM Studio: "none" turns thinking off for hybrid reasoning models; nil leaves the model default
    reasoning_effort = { order = 6, mapping = "parameters", type = "string", optional = true, default = from_preset("reasoning_effort") },
  }
end

local prompts_dir = vim.fs.joinpath(
  vim.fs.dirname(vim.fs.dirname(vim.uv.fs_realpath(debug.getinfo(1, "S").source:sub(2)) or "")),
  "prompts"
)

local function read_prompt(name)
  local fd = io.open(vim.fs.joinpath(prompts_dir, name), "r")
  if not fd then return "" end
  local text = fd:read("*a")
  fd:close()
  return text
end

---@type LazySpec
return {
  {
    "milanglacier/minuet-ai.nvim",
    lazy = true,
    opts = {
      provider = "openai_fim_compatible",
      n_completions = 1,
      context_window = 2048, -- characters around the cursor; the 1.5B FIM model stays under ~150 ms
      request_timeout = request_timeout,
      throttle = 400,
      debounce = 100,
      provider_options = {
        openai_fim_compatible = {
          api_key = "TERM", -- local servers ignore the key, minuet only needs a non-empty env var
          name = cfg.name,
          end_point = cfg.url .. "/v1/completions",
          model = cfg.fim_model,
          template = cfg.fim_template,
          optional = {
            max_tokens = 56,
            temperature = 0.1,
            top_p = 0.9,
            stop = { "\n\n" }, -- small FIM models tend to run past the cursor block
          },
        },
      },
    },
  },
  {
    "Saghen/blink.cmp",
    dependencies = { "milanglacier/minuet-ai.nvim" },
    opts = function(_, opts)
      opts.sources = opts.sources or {}
      opts.sources.default = opts.sources.default or { "lsp", "path", "snippets", "buffer" }
      if not vim.tbl_contains(opts.sources.default, "minuet") then table.insert(opts.sources.default, "minuet") end

      opts.sources.providers = opts.sources.providers or {}
      opts.sources.providers.minuet = {
        name = "minuet",
        module = "minuet.blink",
        async = true,
        timeout_ms = request_timeout * 1000,
        score_offset = 50,
      }

      opts.keymap = opts.keymap or {}
      opts.keymap["<C-]>"] = require("minuet").make_blink_map() -- manual minuet trigger

      opts.completion = opts.completion or {}
      opts.completion.trigger = vim.tbl_deep_extend("force", opts.completion.trigger or {}, {
        prefetch_on_insert = false,
      })
    end,
  },
  {
    "olimorris/codecompanion.nvim",
    dependencies = { "nvim-lua/plenary.nvim", "nvim-treesitter/nvim-treesitter" },
    cmd = { "CodeCompanion", "CodeCompanionChat", "CodeCompanionActions", "CodeCompanionCmd", "AILoad" },
    keys = {
      { "<Leader>ac", "<Cmd>CodeCompanionChat Toggle<CR>", mode = { "n", "v" }, desc = "AI chat" },
      { "<Leader>ai", ":CodeCompanion ", mode = { "n", "v" }, desc = "AI inline" },
      { "<Leader>aa", "<Cmd>CodeCompanionActions<CR>", mode = { "n", "v" }, desc = "AI actions" },
      { "<Leader>as", "<Cmd>CodeCompanionChat Add<CR>", mode = "v", desc = "AI add selection to chat" },
    },
    opts = {
      adapters = {
        http = {
          lmstudio = function()
            return require("codecompanion.adapters").extend("openai_compatible", {
              name = "lmstudio",
              formatted_name = "LM Studio",
              env = {
                url = backends.lmstudio.url,
                api_key = "TERM",
                chat_url = "/v1/chat/completions",
              },
              schema = generation_schema(),
            })
          end,
        },
      },
      interactions = {
        chat = {
          adapter = { name = cfg.adapter, model = cfg.chat_model },
          tools = {
            groups = {
              agent = { system_prompt = agent_system_prompt },
            },
            opts = {
              -- Default 30k would fill a 32k window with a single tool result
              max_output_tokens = 6000,
            },
          },
          opts = {
            system_prompt = function(ctx)
              return ctx.default_system_prompt
                .. ("\nAll non-code text responses must be written in the %s language.\nThe working directory is %s.\n"):format(
                  ctx.language,
                  ctx.cwd
                )
                .. project_rules()
            end,
            context_management = {
              compaction = { min_token_savings = 4000 },
            },
          },
        },
        inline = { adapter = { name = cfg.adapter, model = cfg.chat_model } },
        cmd = { adapter = { name = cfg.adapter, model = cfg.chat_model } },
      },
      -- Rules travel in the system prompt; preset rule files (incl. ~/.claude/CLAUDE.md) stay local
      rules = {
        default = { enabled = false },
        opts = {
          chat = { autoload = {} },
          inline = { autoload = {} },
        },
      },
      opts = {
        language = "Russian",
      },
      prompt_library = {
        ["Bug fixer"] = {
          interaction = "chat",
          description = "Day2 bug-fixer profile: symptom -> root cause, failing test, minimal fix, full check",
          opts = { alias = "bugfix", is_slash_cmd = true, auto_submit = false },
          tools = { "agent" },
          prompts = {
            { role = "system", content = function() return read_prompt("bug-fixer.md") end },
            { role = "user", content = "Симптом:\n" },
          },
        },
      },
    },
    config = function(_, opts)
      require("codecompanion").setup(opts)

      vim.api.nvim_create_user_command("AILoad", function()
        local cmd = { "lms", "load", chat_model, "-c", tostring(preset.context_window), "-y", "--identifier", chat_model }
        vim.notify("AILoad: " .. table.concat(cmd, " "))
        vim.system(cmd, { text = true }, function(res)
          vim.schedule(function()
            local level = res.code == 0 and vim.log.levels.INFO or vim.log.levels.ERROR
            vim.notify(("AILoad exit %d\n%s"):format(res.code, res.stderr or ""), level)
          end)
        end)
      end, { desc = "Load the chat model into LM Studio with the configured context length" })

      -- Attach the buffer the chat was opened from, same as typing #{buffer} (kept in sync)
      vim.api.nvim_create_autocmd("User", {
        pattern = "CodeCompanionChatCreated",
        group = vim.api.nvim_create_augroup("codecompanion_auto_buffer", { clear = true }),
        callback = function(args)
          local chat = require("codecompanion").buf_get_chat(args.data.bufnr)
          local bufnr = chat and chat.buffer_context and chat.buffer_context.bufnr
          if not (bufnr and vim.api.nvim_buf_is_valid(bufnr)) then return end
          if vim.bo[bufnr].buftype ~= "" or vim.api.nvim_buf_get_name(bufnr) == "" then return end

          -- internal module: re-check after codecompanion updates
          local ok, err = pcall(function()
            require("codecompanion.interactions.shared.editor_context.buffer")
              .new({ Chat = chat, params = "diff" })
              :chat_render({ bufnr = bufnr })
          end)
          if not ok then vim.notify("codecompanion auto buffer: " .. tostring(err), vim.log.levels.WARN) end
        end,
      })
    end,
  },
}
