local _, ns = ...

local REFRESH = 15 * 60

local function capture(why)
    if ns.InCombat() then return ns.AfterCombat("rankNames:" .. tostring(why), function() capture(why) end) end
    if not ns.Enabled("rankNames") then return end
    if not ns.Call("IsInGuild") then
        ns.Debug("rank names (" .. why .. "): not in a guild")
        return
    end
    local guildName = ns.Call("GetGuildInfo", "player")
    local count = ns.Call("GuildControlGetNumRanks")
    if not guildName or not count or count < 1 then
        ns.Debug("rank names (" .. why .. "): guild " .. tostring(guildName) .. ", " .. tostring(count) .. " ranks")
        return
    end
    local ranks = {}
    for i = 1, count do
        local rankName = ns.Call("GuildControlGetRankName", i)
        if rankName and rankName ~= "" then
            local rank = { index = i - 1, name = ns.Clean(rankName, 40) }
            local flags
            if ns.Func("C_GuildInfo.GuildControlGetRankFlags") then
                local t = ns.Call("C_GuildInfo.GuildControlGetRankFlags", i)
                if type(t) == "table" and not ns.IsSecret(t) then flags = t end
            elseif ns.Func("GuildControlGetRankFlags") then
                flags = { ns.Call("GuildControlGetRankFlags", i) }
            end
            if flags then
                local names = {}
                for j = 1, math.min(#flags, 40) do
                    if flags[j] == true then
                        local text = _G["GUILDCONTROL_OPTION" .. j]
                        names[#names + 1] = ns.Clean(type(text) == "string" and text or tostring(j), 60)
                    end
                end
                if #flags > 0 then rank.flags = names end
            end
            ranks[#ranks + 1] = rank
        end
    end
    if #ranks == 0 then return end
    local c = ns.Character()
    if not c then return end
    ns.Debug("rank names (" .. why .. "): " .. #ranks .. " ranks")
    ns.SetState("rankNames", guildName .. "-" .. c.realm, {
        guild = ns.Clean(guildName, 60),
        ranks = ranks,
    })
end

local function requestRoster()
    if ns.InCombat() then return ns.AfterCombat("rankNames:request", function() requestRoster() end) end
    if not (ns.Enabled("rankNames") or ns.Enabled("guildRoster")) or not ns.Call("IsInGuild") then return end
    if C_GuildInfo and C_GuildInfo.GuildRoster then
        C_GuildInfo.GuildRoster()
    elseif type(GuildRoster) == "function" then
        GuildRoster()
    else
        ns.NoteMissing("GuildRoster")
        return
    end
    ns.Debug("rank names: roster requested")
end

ns.On("PLAYER_LOGIN", function()
    if not C_Timer then return end
    C_Timer.After(10, function()
        capture("login")
        requestRoster()
    end)
    if C_Timer.NewTicker then C_Timer.NewTicker(REFRESH, requestRoster) end
end, "RankNames.login")
for _, event in ipairs({ "GUILD_RANKS_UPDATE", "GUILD_ROSTER_UPDATE", "PLAYER_GUILD_UPDATE" }) do
    ns.On(event, function() capture(event) end, "RankNames.capture")
end
ns.On("PLAYER_LOGOUT", function() capture("logout") end, "RankNames.capture")
