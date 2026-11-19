local _, ns = ...

local function status()
    ns.Print("WoW Forever Guild Chronicle v" .. ns.VERSION .. " is running.")
    ns.Print(ns.AppLine())
    ns.Print("Your data is saved when you log out or reload. Type /wft help for the commands.")
end

local function showDebug(lines)
    local log = ns.db.diag.log
    ns.Print("diagnostics (last " .. math.min(lines, #log) .. " of " .. #log .. " lines; WFT Companion's Save logs has them all):")
    for i = math.max(1, #log - lines + 1), #log do ns.Print("  " .. log[i]) end
    local missing = {}
    for name in pairs(ns.db.diag.missing) do missing[#missing + 1] = name end
    table.sort(missing)
    if ns.ProbeSummary then ns.Print("probe this session: " .. ns.ProbeSummary()) end
    ns.Print("missing on this client: " .. (#missing == 0 and "nothing" or table.concat(missing, ", ")))
    ns.Print("/wft debug all - whole log, /wft debug chat - echo new lines to chat")
end

local function help()
    ns.Print("/wft: the addon's version, the Companion app and when your data is saved")
    ns.Print("/wft save on|off: ask to save (reload) at safe moments, or stop asking (now " .. (ns.SavePromptOn() and "on" or "off") .. ")")
    ns.Print("/wft debug: the last 20 lines of the diagnostics log, for support")
    ns.Print("/wft debug all: the whole diagnostics log")
    ns.Print("/wft debug chat: show new diagnostics lines in chat, and type it again to stop")
end

SLASH_WOWFOREVERGUILDCHRONICLE1 = "/wft"
SLASH_WOWFOREVERGUILDCHRONICLE2 = "/wfgc"
SlashCmdList.WOWFOREVERGUILDCHRONICLE = function(msg)
    if not ns.db then return end
    local cmd, arg = (msg or ""):lower():match("^%s*(%S*)%s*(%S*)")
    if cmd == "debug" and arg == "chat" then
        ns.db.debugChat = not ns.db.debugChat
        ns.Print("diagnostics in chat " .. (ns.db.debugChat and "on" or "off"))
    elseif cmd == "debug" then
        showDebug(arg == "all" and ns.LOG_LINES or 20)
    elseif cmd == "save" and (arg == "on" or arg == "off") then
        ns.db.savePrompt = arg == "on"
        ns.Print("asking to save at safe moments: " .. arg)
    elseif cmd == "save" then
        ns.Print("asking to save at safe moments: " .. (ns.SavePromptOn() and "on" or "off") .. " (/wft save on or /wft save off)")
    elseif cmd == "help" then
        help()
    else
        status()
    end
end

ns.lastFileAt = debugprofilestop and debugprofilestop() or nil
