-- Headless driver: one codecompanion chat with the IDE config (Day4/nvim/ai.lua), @{agent} tools,
-- approval_mode "yolo", the task as the only user message. No follow-ups: whatever the model does
-- before its first final answer is the result. Started by run.sh as
--   nvim --headless -i NONE -n -c "luafile driver.lua"
-- Env: BENCH_TASK (file), BENCH_SYSTEM (optional file, extra system message), BENCH_OUT (dir),
--      BENCH_MAX_WALL_S, BENCH_MAX_REQUESTS, BENCH_DRY=1 (dump the request messages, do not submit).

local out = assert(vim.env.BENCH_OUT, "BENCH_OUT is required")
local max_wall_s = tonumber(vim.env.BENCH_MAX_WALL_S or "") or 2700
local max_requests = tonumber(vim.env.BENCH_MAX_REQUESTS or "") or 120
local dry = vim.env.BENCH_DRY == "1"

local uv = vim.uv
local t0 = uv.hrtime()
local function elapsed_ms() return math.floor((uv.hrtime() - t0) / 1e6) end

local function read(path)
  local fd = assert(io.open(path, "r"), "cannot read " .. path)
  local text = fd:read("*a")
  fd:close()
  return text
end

local function write(name, text)
  local fd = assert(io.open(vim.fs.joinpath(out, name), "w"))
  fd:write(text)
  fd:close()
end

local events = {}
local function event(kind, data)
  local e = vim.tbl_extend("force", { t_ms = elapsed_ms(), kind = kind }, data or {})
  table.insert(events, vim.json.encode(e))
end

local state = { requests = 0, tool_calls = {}, status = "running", finished = false }

local ok_cc, cc = pcall(require, "codecompanion")
if not ok_cc then
  write("meta.json", vim.json.encode({ status = "error", error = "codecompanion not loadable: " .. tostring(cc) }))
  vim.cmd("qa!")
  return
end
local config = require("codecompanion.config")
local Chat = require("codecompanion.interactions.chat")

-- Nobody answers questions in a headless run (same as Day2, where AskUserQuestion was absent)
local agent = config.interactions.chat.tools.groups.agent
agent.tools = vim.tbl_filter(function(t) return t ~= "ask_questions" end, agent.tools)

local chat ---@type CodeCompanion.Chat

local finish_impl

local function finish(status, err)
  if state.finished then return end
  state.finished = true
  local ok, e = pcall(finish_impl, status, err)
  if not ok then io.stderr:write("driver: finish failed: " .. tostring(e) .. "\n") end
  vim.schedule(function() vim.cmd("qa!") end)
end

finish_impl = function(status, err)
  state.status = status

  local messages = chat and chat.messages or {}
  local final = ""
  for i = #messages, 1, -1 do
    local m = messages[i]
    if m.role == "llm" or m.role == "assistant" then
      if type(m.content) == "string" and m.content ~= "" then
        final = m.content
        break
      end
    end
  end

  local adapter = chat and chat.adapter or {}
  write("answer.md", final)
  write("messages.json", vim.json.encode(messages))
  write("events.jsonl", table.concat(events, "\n") .. "\n")
  if chat and chat.bufnr and vim.api.nvim_buf_is_valid(chat.bufnr) then
    write("transcript.md", table.concat(vim.api.nvim_buf_get_lines(chat.bufnr, 0, -1, false), "\n"))
  end
  write(
    "meta.json",
    vim.json.encode({
      status = status,
      error = err,
      wall_ms = elapsed_ms(),
      requests = state.requests,
      tool_calls = state.tool_calls,
      tokens = chat and chat.tokens or nil,
      model = adapter.schema and adapter.schema.model and adapter.schema.model.default,
      parameters = adapter.parameters,
      context_window = vim.env.AI_CONTEXT_WINDOW,
    })
  )
end

local function on(pattern, fn)
  vim.api.nvim_create_autocmd("User", { pattern = pattern, callback = fn })
end

on("CodeCompanionRequestStarted", function()
  state.requests = state.requests + 1
  event("request_started", { n = state.requests })
  if state.requests > max_requests then
    event("limit", { what = "requests" })
    pcall(function() chat:stop() end)
    finish("request_limit")
  end
end)
on("CodeCompanionRequestFinished", function(args)
  event("request_finished", { status = args.data and args.data.status, tokens = chat and chat.tokens })
end)
on("CodeCompanionToolStarted", function(args)
  local name = args.data and args.data.tool or "?"
  state.tool_calls[name] = (state.tool_calls[name] or 0) + 1
  event("tool_started", { tool = name, args = args.data and args.data.args })
end)
on("CodeCompanionToolFinished", function(args)
  event("tool_finished", { tool = args.data and args.data.tool })
end)
on("CodeCompanionChatDone", function()
  event("chat_done")
  finish("done")
end)
on("CodeCompanionChatStopped", function()
  event("chat_stopped")
  finish(state.status == "running" and "stopped" or state.status)
end)

vim.defer_fn(function()
  if state.finished then return end
  event("limit", { what = "wall" })
  pcall(function() chat:stop() end)
  finish("timeout")
end, max_wall_s * 1000)

local messages = {}
if vim.env.BENCH_SYSTEM and vim.env.BENCH_SYSTEM ~= "" then
  table.insert(messages, { role = config.constants.SYSTEM_ROLE, content = read(vim.env.BENCH_SYSTEM) })
end
table.insert(messages, { role = config.constants.USER_ROLE, content = read(assert(vim.env.BENCH_TASK)) })

chat = Chat.new({
  buffer_context = require("codecompanion.utils.context").get(vim.api.nvim_get_current_buf()),
  messages = messages,
  tools = { "agent" },
  approval_mode = "yolo",
  auto_submit = false,
  stop_context_insertion = true,
})
if not chat then
  write("meta.json", vim.json.encode({ status = "error", error = "chat not created" }))
  vim.cmd("qa!")
  return
end

if dry then
  -- What the adapter would send: system prompt(s), tool prompt, task
  write("request-messages.json", vim.json.encode(chat.messages))
  write("tools.json", vim.json.encode(vim.tbl_keys(chat.tool_registry and chat.tool_registry.in_use or {})))
  state.finished = true
  vim.cmd("qa!")
  return
end

event("submit")
chat:submit()
