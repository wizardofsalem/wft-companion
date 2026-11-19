local addonName, ns = ...

local PROBE_FUNCTIONS = {
    "GetLootSourceInfo", "GetLootSlotInfo", "GetLootSlotLink", "GetLootSlotType", "IsFishingLoot",
    "GetItemInfo", "GetTradeSkillLine", "GetTradeSkillInfo", "GetTradeSkillItemLink",
    "GetTradeSkillRecipeLink", "GetCraftDisplaySkillLine", "GetCraftInfo", "GetCraftItemLink",
    "GetCraftRecipeLink", "GetSkillLineInfo", "ExpandSkillHeader", "GuildControlGetNumRanks",
    "GuildControlGetRankName", "GetFactionInfo", "ExpandFactionHeader", "RequestTimePlayed",
    "CombatLogGetCurrentEventInfo", "GetMoney", "GetInboxItem", "GetInboxItemLink", "TakeInboxItem",
    "AutoLootMailItem", "GetTradeTargetItemLink", "GetTradeTargetItemInfo", "RepairAllItems",
    "hooksecurefunc",
}
local PROBE_STRINGS = {
    "LOOT_ITEM_SELF", "LOOT_ITEM_SELF_MULTIPLE", "LOOT_ITEM_PUSHED_SELF", "LOOT_ITEM_PUSHED_SELF_MULTIPLE",
    "LOOT_ITEM_CREATED_SELF", "LOOT_ITEM_CREATED_SELF_MULTIPLE", "LOOT_ROLL_YOU_WON", "ERR_TRADE_COMPLETE",
}

ns.On("PLAYER_LOGIN", function()
    local diag = ns.db.diag
    local version, build, _, interface = ns.Call("GetBuildInfo")
    diag.build = tostring(version) .. " (" .. tostring(build) .. ", interface " .. tostring(interface) .. ")"
    diag.locale = ns.Call("GetLocale")
    local c = ns.Character()
    ns.Log("info", "login " .. (c and ns.CharKey(c) or "?") .. " level " .. tostring(c and c.level)
        .. ", client " .. diag.build .. " " .. tostring(diag.locale))
    local missing = {}
    for _, name in ipairs(PROBE_FUNCTIONS) do
        if type(_G[name]) ~= "function" then
            ns.NoteMissing(name)
            missing[#missing + 1] = name
        end
    end
    if type(C_Timer) ~= "table" then ns.NoteMissing("C_Timer") end
    if not (type(C_PartyInfo) == "table" and C_PartyInfo.GetLootMethod) and type(GetLootMethod) ~= "function" then
        ns.NoteMissing("GetLootMethod")
    end
    local bags = type(C_Container) == "table" and C_Container.GetContainerItemInfo or _G.GetContainerItemInfo
    if type(bags) ~= "function" then ns.NoteMissing("GetContainerItemInfo") end
    for _, name in ipairs(PROBE_STRINGS) do
        if type(_G[name]) ~= "string" then
            ns.NoteMissing(name)
            missing[#missing + 1] = name
        end
    end
    ns.Log("info", "probe: " .. (#missing == 0 and "all game functions present" or (#missing .. " missing")))
end, "Diagnostics.probe")

local function summary()
    local parts = {}
    for _, cat in ipairs(ns.CATEGORIES) do
        local n = ns.HistoryCount(cat)
        for _ in pairs(ns.db.state[cat] or {}) do n = n + 1 end
        parts[#parts + 1] = cat .. "=" .. n
    end
    ns.Log("info", "logout; stored " .. table.concat(parts, " "))
end

ns.On("PLAYER_LOGIN", function() ns.On("PLAYER_LOGOUT", summary, "Diagnostics.logout") end, "Diagnostics.summary")

local BLOCKED_LINES = 20
local RECENT_CALLS = 5
local blockedLines, noting = 0, false

local function restrictions()
    local kinds = type(Enum) == "table" and Enum.AddOnRestrictionType
    local active = type(C_RestrictedActions) == "table" and C_RestrictedActions.IsAddOnRestrictionActive
    if type(kinds) ~= "table" or type(active) ~= "function" then return "?" end
    local on = {}
    for name, kind in pairs(kinds) do
        local ok, v = pcall(active, kind)
        if ok and not ns.IsSecret(v) and v == true then on[#on + 1] = tostring(name) end
    end
    table.sort(on)
    return #on == 0 and "none" or table.concat(on, "+")
end

local function noteBlocked(event, what, calling)
    local t = type(GetTime) == "function" and GetTime() or 0
    local recent = {}
    for i = #ns.recentCalls, math.max(1, #ns.recentCalls - RECENT_CALLS + 1), -1 do
        local c = ns.recentCalls[i]
        recent[#recent + 1] = c.name .. " " .. string.format("%.1f", t - c.t) .. "s"
    end
    ns.Log("warn", "blocked action: " .. event .. " " .. (ns.Clean(what, 80) or "?")
        .. "; during " .. (calling or "-")
        .. "; combat " .. (ns.InCombat() and "yes" or "no")
        .. "; restricted " .. restrictions()
        .. "; last calls " .. (#recent == 0 and "-" or table.concat(recent, ", ")))
end

for _, event in ipairs({ "ADDON_ACTION_FORBIDDEN", "ADDON_ACTION_BLOCKED" }) do
    ns.On(event, function(addon, what)
        if addon ~= addonName or noting or blockedLines >= BLOCKED_LINES then return end
        local calling = ns.calling
        noting = true
        blockedLines = blockedLines + 1
        local ok, err = pcall(noteBlocked, event, what, calling)
        if not ok then ns.Log("warn", "blocked action: " .. event .. " (not described: " .. tostring(err) .. ")") end
        if blockedLines == BLOCKED_LINES then ns.Log("warn", "blocked action: no more are logged this session") end
        noting = false
    end, "Diagnostics.blocked")
end

ns.On("UI_ERROR_MESSAGE", function(a, b)
    if ns.Active("merchant") then
        ns.Log("info", "vendor error: " .. tostring(ns.Clean(type(b) == "string" and b or a, 80)))
    end
end, "Diagnostics.vendorError")
