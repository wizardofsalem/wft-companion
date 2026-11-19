local _, ns = ...

local THROTTLE = 60
local lastAt

local function strip(s)
    if type(s) ~= "string" or ns.IsSecret(s) then return nil end
    s = s:gsub("|c%x%x%x%x%x%x%x%x", ""):gsub("|r", ""):gsub("|H.-|h(.-)|h", "%1"):gsub("^%[(.*)%]$", "%1")
    s = ns.Clean(s, 80)
    return s ~= "" and s or nil
end

local function kindName(t)
    if type(t) == "number" then
        local e = type(Enum) == "table" and Enum.GuildNewsType
        if type(e) == "table" then
            for k, v in pairs(e) do if v == t then return ns.Clean(k, 20) end end
        end
        return tostring(t)
    end
    if type(t) == "string" and not ns.IsSecret(t) then return ns.Clean(t, 20) end
end

local function capture(why, force)
    if ns.InCombat() then return ns.AfterCombat("guildNews:" .. tostring(why), function() capture(why, force) end) end
    if not ns.Enabled("guildNews") or not ns.Call("IsInGuild") then return end
    if not ns.Func("GetNumGuildNews") or not ns.Func("C_GuildInfo.GetGuildNewsInfo") then return end
    if not force and lastAt and GetTime() - lastAt < THROTTLE then return end
    local guildName = ns.Call("GetGuildInfo", "player")
    local c = ns.Character()
    if not guildName or not c then return end
    lastAt = GetTime()
    local entries = {}
    for i = 1, math.min(tonumber((ns.Call("GetNumGuildNews"))) or 0, 200) do
        local n = ns.Call("C_GuildInfo.GetGuildNewsInfo", i)
        if type(n) == "table" and not ns.IsSecret(n) and not n.isHeader then
            local kind = kindName(n.newsType)
            if kind then
                local e = { kind = kind, player = strip(n.whoText) }
                if type(n.whatText) == "string" and not ns.IsSecret(n.whatText) then e.itemId = ns.ItemIdFromLink(n.whatText) end
                local data = tonumber(n.newsDataID)
                if data and data > 0 and not ns.IsSecret(data) and kind:lower():find("achievement", 1, true) then e.achievementId = math.floor(data) end
                local lvl = type(n.data) == "table" and tonumber(n.data[1]) or tonumber(n.data)
                if lvl and lvl > 0 and lvl < 1000 and not ns.IsSecret(lvl) and kind:lower():find("level", 1, true) then e.level = math.floor(lvl) end
                entries[#entries + 1] = e
            end
        end
    end
    if #entries == 0 then return end
    if ns.SetState("guildNews", guildName .. "-" .. c.realm, { guild = ns.Clean(guildName, 60), entries = entries }) then
        ns.Debug("guild news (" .. why .. "): " .. #entries .. " entries")
    end
end

ns.On("GUILD_NEWS_UPDATE", function() capture("update") end, "GuildNews.update")
ns.On("PLAYER_LOGIN", function()
    if not (C_Timer and C_Timer.After) then return end
    C_Timer.After(12, function() ns.AfterCombat("guildNews:query", function()
        if ns.Func("C_GuildInfo.QueryGuildNews") then ns.Call("C_GuildInfo.QueryGuildNews")
        elseif ns.Func("QueryGuildNews") then ns.Call("QueryGuildNews") end
        C_Timer.After(5, function() capture("login", true) end)
    end) end)
end, "GuildNews.login")
