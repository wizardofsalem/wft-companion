local _, ns = ...

local THROTTLE = 60
local MAX = 500
local lastAt = 0

local function text(v)
    if type(v) ~= "string" or ns.IsSecret(v) then return nil end
    return v
end

local function flat(s)
    s = s:gsub("[\r\n]+", " ")
    s = ns.Clean(s, MAX)
    return s ~= "" and s or nil
end

local function guildText(member, global)
    if ns.Func("C_GuildInfo." .. member) then
        local v = text(ns.Call("C_GuildInfo." .. member))
        if v ~= nil then return v end
    end
    if ns.Func(global) then return text(ns.Call(global)) end
end

local function capture(why, force, motdArg)
    if ns.InCombat() then return ns.AfterCombat("guildInfo:" .. tostring(why), function() capture(why, force, motdArg) end) end
    if not ns.Enabled("guildInfo") or not ns.Call("IsInGuild") then return end
    if not force and time() - lastAt < THROTTLE then return end
    local guildName = ns.Call("GetGuildInfo", "player")
    local c = ns.Character()
    if not guildName or not c then return end
    local motd = text(motdArg) or guildText("GetMOTD", "GetGuildRosterMOTD")
    local info = guildText("GetInfoText", "GetGuildInfoText")
    if motd == nil and info == nil then return end
    lastAt = time()
    local key = guildName .. "-" .. c.realm
    local old = (ns.db.state.guildInfo or {})[key]
    local fields = { guild = ns.Clean(guildName, 60) }
    if motd ~= nil then
        fields.motdLength = #motd
        fields.motd = flat(motd)
    elseif old then
        fields.motdLength = old.motdLength
        fields.motd = old.motd
    end
    if info ~= nil then
        fields.infoLength = #info
        fields.info = flat(info)
    elseif old then
        fields.infoLength = old.infoLength
        fields.info = old.info
    end
    local changed = not old or old.motd ~= fields.motd or old.info ~= fields.info
        or old.motdLength ~= fields.motdLength or old.infoLength ~= fields.infoLength
    fields.changedAt = changed and time() or old.changedAt
    if ns.SetState("guildInfo", key, fields) then
        ns.Debug("guild info (" .. why .. "): motd " .. tostring(fields.motdLength) .. ", info " .. tostring(fields.infoLength))
    end
end

ns.On("GUILD_MOTD", function(motd) capture("motd", true, motd) end, "GuildInfo.motd")
ns.On("GUILD_ROSTER_UPDATE", function() capture("update") end, "GuildInfo.update")
ns.On("PLAYER_GUILD_UPDATE", function() capture("guild", true) end, "GuildInfo.guild")
ns.On("PLAYER_LOGIN", function()
    if C_Timer and C_Timer.After then C_Timer.After(15, function() capture("login", true) end) end
end, "GuildInfo.login")
