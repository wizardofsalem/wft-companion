local _, ns = ...

local THROTTLE = 60
local EVERY = 3600
local lastRead

local function offlineHours(i)
    local years, months, days, hours = ns.Call("GetGuildRosterLastOnline", i)
    if not years and not months and not days and not hours then return nil end
    return ns.AgeHours(years, months, days, hours)
end

local function capture(why, force)
    if ns.InCombat() then return ns.AfterCombat("guildRoster:" .. tostring(why), function() capture(why, force) end) end
    if not ns.Enabled("guildRoster") then return end
    if not force and lastRead and GetTime() - lastRead < THROTTLE then return end
    if not ns.Call("IsInGuild") or not ns.Func("GetGuildRosterInfo") then return end
    local guildName = ns.Call("GetGuildInfo", "player")
    local total = tonumber((ns.Call("GetNumGuildMembers"))) or 0
    local c = ns.Character()
    if not guildName or total < 1 or not c then
        ns.Debug("guild roster (" .. why .. "): nothing to read")
        return
    end
    local guildKey = guildName .. "-" .. c.realm
    ns.db.rosterAt = ns.db.rosterAt or {}
    local stored = ns.db.state and ns.db.state.guildRoster and ns.db.state.guildRoster[guildKey]
    if not force and stored and stored.complete and ns.db.rosterAt[guildKey]
        and time() - ns.db.rosterAt[guildKey] < EVERY then return end
    lastRead = GetTime()
    local members, online = {}, 0
    for i = 1, total do
        local name, rankName, rankIndex, level, _, _, _, _, isOnline, status, classFile, _, _, isMobile, _, _, guid =
            ns.Call("GetGuildRosterInfo", i)
        if name then
            local m = {
                name = ns.Clean(name, 60),
                guid = guid,
                rankIndex = tonumber(rankIndex),
                level = tonumber(level),
                class = classFile,
                online = isOnline and true or false,
            }
            if type(rankName) == "string" and not ns.IsSecret(rankName) and rankName ~= "" then m.rankName = ns.Clean(rankName, 40) end
            local st = status
            if st == "<AFK>" then st = 1 elseif st == "<DND>" then st = 2 end
            st = tonumber(st)
            if st and st >= 0 and st <= 2 then m.status = st end
            if isMobile == true then m.mobile = true end
            if isOnline then
                online = online + 1
            else
                m.offlineHours = offlineHours(i)
            end
            members[#members + 1] = m
        end
    end
    if #members == 0 then return end
    table.sort(members, function(a, b) return a.name < b.name end)
    local showOffline = ns.Call("GetGuildRosterShowOffline")
    ns.db.rosterAt[guildKey] = time()
    local r = ns.SetState("guildRoster", guildKey, {
        guild = ns.Clean(guildName, 60),
        memberCount = total,
        complete = showOffline ~= false and #members >= total,
        members = members,
    })
    if r then ns.Debug("guild roster (" .. why .. "): " .. #members .. " members, " .. online .. " online") end
end

ns.On("GUILD_ROSTER_UPDATE", function() capture("update") end, "GuildRoster.update")
ns.On("PLAYER_GUILD_UPDATE", function() capture("guild", true) end, "GuildRoster.guild")
