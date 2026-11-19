local _, ns = ...

local KINDS = { invite = true, join = true, promote = true, demote = true, remove = true, quit = true }
local MAX_EVENTS = 100
local asked = false

local function capture()
    if ns.InCombat() then return ns.AfterCombat("guildLog:read", function() capture() end) end
    if not ns.Enabled("guildLog") or not ns.Call("IsInGuild") then return end
    local guildName = ns.Call("GetGuildInfo", "player")
    local count = tonumber((ns.Call("GetNumGuildEvents"))) or 0
    local c = ns.Character()
    if not guildName or count < 1 or not c then return end
    local events = {}
    for i = math.max(1, count - MAX_EVENTS + 1), count do
        local kind, player1, player2, rank, years, months, days, hours = ns.Call("GetGuildEventInfo", i)
        if kind then
            local e = { kind = KINDS[kind] and kind or "unknown", hoursAgo = ns.AgeHours(years, months, days, hours) }
            if kind == "join" or kind == "quit" then
                e.player = ns.Clean(player1, 60)
            else
                e.by, e.player = ns.Clean(player1, 60), ns.Clean(player2, 60)
            end
            if rank ~= nil and rank ~= "" then e.rank = ns.Clean(rank, 40) end
            events[#events + 1] = e
        end
    end
    if #events == 0 then return end
    if ns.SetState("guildLog", guildName .. "-" .. c.realm, {
        guild = ns.Clean(guildName, 60),
        events = events,
        entries = count,
    }) then
        ns.Debug("guild log: " .. #events .. " events")
    end
end

local function ask()
    if ns.InCombat() then return ns.AfterCombat("guildLog:ask", function() ask() end) end
    if asked or not ns.Enabled("guildLog") or not ns.Call("IsInGuild") then return end
    if not ns.Func("QueryGuildEventLog") then
        ns.NoteMissing("QueryGuildEventLog")
        return
    end
    asked = true
    ns.Call("QueryGuildEventLog")
end

ns.On("PLAYER_LOGIN", function()
    if C_Timer then C_Timer.After(30, ask) end
end, "GuildLog.login")
ns.On("GUILD_EVENT_LOG_UPDATE", capture, "GuildLog.update")
