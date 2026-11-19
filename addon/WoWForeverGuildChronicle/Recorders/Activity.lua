local _, ns = ...

local function safe(fn)
    return function(...)
        local ok, err = pcall(fn, ...)
        if not ok then ns.Log("error", "Activity: " .. tostring(err)) end
    end
end


local combatStart, restStart

local function endCombat()
    if not combatStart then return end
    local seconds = math.floor(GetTime() - combatStart + 0.5)
    combatStart = nil
    if seconds > 0 and seconds < 86400 then ns.DayCount("combatSeconds", seconds) end
end

local function endRest()
    if not restStart then return end
    local seconds = math.floor(GetTime() - restStart + 0.5)
    restStart = nil
    if seconds > 0 and seconds < 86400 then ns.DayCount("restingSeconds", seconds) end
end

ns.On("PLAYER_REGEN_DISABLED", safe(function()
    if combatStart then return end
    combatStart = GetTime()
    ns.DayCount("combats")
end), "Activity.combatStart")
ns.On("PLAYER_REGEN_ENABLED", safe(endCombat), "Activity.combatEnd")
ns.On("PLAYER_UPDATE_RESTING", safe(function()
    if ns.Call("IsResting") == true then
        restStart = restStart or GetTime()
    else
        endRest()
    end
end), "Activity.rest")
ns.On("PLAYER_ENTERING_WORLD", safe(function()
    combatStart = nil
    if ns.Call("IsResting") == true then restStart = restStart or GetTime() else restStart = nil end
end), "Activity.enter")


local function lines(sentField, heardField)
    return safe(function(_, sender, _, _, _, _, _, _, _, _, _, guid)
        ns.DayCount(heardField)
        local c = ns.Character()
        if not c then return end
        if guid and ns.IsSecret(guid) == false and c.guid ~= "" and guid == c.guid then
            ns.DayCount(sentField)
        elseif not guid and type(sender) == "string" and not ns.IsSecret(sender) then
            local full = c.surname and (c.name .. " " .. c.surname)
            if sender == c.name or (full and ns.SamePlayer(sender, full)) then ns.DayCount(sentField) end
        end
    end)
end
ns.On("CHAT_MSG_PARTY", lines("partySent", "partyHeard"), "Activity.party")
ns.On("CHAT_MSG_PARTY_LEADER", lines("partySent", "partyHeard"), "Activity.partyLeader")


local MAX_SPELLS = 300
local counts = {}

local function flush()
    if not ns.Enabled("spellUse") then counts = {}; return end
    local c = ns.Character()
    if not c then return end
    for day, spells in pairs(counts) do
        local key = ns.StateKey(c) .. "|" .. day
        local old = (ns.db.state.spellUse or {})[key]
        local merged, n = {}, 0
        for _, e in ipairs(old and old.casts or {}) do merged[e.spellId] = e.count; n = n + 1 end
        for id, k in pairs(spells) do
            if merged[id] == nil then n = n + 1 end
            if merged[id] ~= nil or n <= MAX_SPELLS then merged[id] = (merged[id] or 0) + k end
        end
        local list = {}
        for id, k in pairs(merged) do list[#list + 1] = { spellId = id, count = k } end
        table.sort(list, function(a, b) return a.spellId < b.spellId end)
        ns.SetState("spellUse", key, { day = day, casts = list })
    end
    counts = {}
    local cutoff = date("%Y-%m-%d", time() - 35 * 86400)
    for k, r in pairs(ns.db.state.spellUse or {}) do
        if type(r) ~= "table" or type(r.day) ~= "string" or r.day < cutoff then ns.db.state.spellUse[k] = nil end
    end
end

ns.On("UNIT_SPELLCAST_SUCCEEDED", safe(function(unit, _, spellId)
    if unit ~= "player" or not ns.Enabled("spellUse") then return end
    spellId = tonumber(spellId)
    if not spellId or ns.IsSecret(spellId) or spellId <= 0 then return end
    local day = date("%Y-%m-%d", time())
    local today = counts[day]
    if not today then today = {}; counts[day] = today end
    today[spellId] = (today[spellId] or 0) + 1
end), "Activity.cast")

ns.On("PLAYER_LOGIN", function()
    if C_Timer and C_Timer.NewTicker then C_Timer.NewTicker(60, safe(flush)) end
end, "Activity.login")
ns.On("PLAYER_LOGOUT", safe(function() endCombat(); endRest(); flush() end), "Activity.logout")
ns.FlushSpellUse = flush
