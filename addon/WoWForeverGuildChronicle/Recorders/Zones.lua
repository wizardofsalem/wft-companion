local _, ns = ...

local KEEP_DAYS = 35
local CHECKPOINT = 15 * 60
local lastDay

local function today() return date("%Y-%m-%d", time()) end

local function prune()
    local bucket = ns.db.state.zones
    if not bucket then return end
    local cutoff = date("%Y-%m-%d", time() - KEEP_DAYS * 86400)
    for key, r in pairs(bucket) do
        if type(r) ~= "table" or type(r.day) ~= "string" or r.day < cutoff then bucket[key] = nil end
    end
    for key, t in pairs(ns.db.openZone or {}) do
        if type(t) ~= "table" or type(t.day) ~= "string" or t.day < cutoff then ns.db.openZone[key] = nil end
    end
end

local function track(c, day)
    ns.db.openZone = ns.db.openZone or {}
    local key = ns.StateKey(c)
    local t = ns.db.openZone[key]
    if t and t.day == day then return t, key end
    local old = (ns.db.state.zones or {})[key .. "|" .. day]
    t = { day = day, zones = {}, subs = {}, per = {}, cur = nil, since = nil }
    if old and old.day == day then
        for i, n in ipairs(old.zones or {}) do t.zones[i] = n end
        for i, n in ipairs(old.subzones or {}) do t.subs[i] = n end
        for _, e in ipairs(old.perZone or {}) do
            if type(e) == "table" and type(e.zone) == "string" then
                t.per[e.zone] = { seconds = e.seconds or 0, xp = e.xp, kills = e.kills, quests = e.quests,
                    questXp = e.questXp, hks = e.hks }
            end
        end
    end
    local prev = ns.db.openZone[key]
    if prev and prev.cur then t.cur, t.since = prev.cur, prev.since end
    ns.db.openZone[key] = t
    return t, key
end

local function has(list, v)
    for _, n in ipairs(list) do if n == v then return true end end
end

local COUNTERS = { "xp", "kills", "quests", "questXp", "hks" }

local function flush(c, t, key)
    if not ns.Enabled("zones") then return end
    local per = {}
    for _, name in ipairs(t.zones) do
        local e = t.per[name]
        local row = { zone = name, seconds = math.floor((e and e.seconds or 0) + 0.5) }
        if e then
            for _, f in ipairs(COUNTERS) do
                local v = e[f]
                if type(v) == "number" and v > 0 then row[f] = math.floor(v) end
            end
        end
        per[#per + 1] = row
    end
    ns.SetState("zones", key .. "|" .. t.day, {
        day = t.day, count = #t.zones, zones = t.zones, subzoneCount = #t.subs, subzones = t.subs, perZone = per,
    })
end

local function zoneName()
    local zone = ns.Call("GetRealZoneText")
    if type(zone) ~= "string" or ns.IsSecret(zone) then return nil end
    zone = ns.Clean(zone, 60)
    if not zone or zone == "" then return nil end
    return zone
end

local function entry(t, zone)
    if not has(t.zones, zone) then t.zones[#t.zones + 1] = zone end
    local e = t.per[zone]
    if not e then
        e = { seconds = 0 }
        t.per[zone] = e
    end
    return e
end

local function credit(t, at)
    if t.cur and t.since and at > t.since then
        entry(t, t.cur).seconds = entry(t, t.cur).seconds + (at - t.since)
    end
    t.since = at
end

local function visit(sub, at)
    if not ns.Enabled("zones") then return end
    local c = ns.Character()
    if not c then return end
    local zone = zoneName()
    if not zone then return end
    at = at or time()
    local day = today()
    if day ~= lastDay then
        prune()
        lastDay = day
    end
    local t, key = track(c, day)
    local before = #t.zones + #t.subs
    if t.cur ~= zone then
        credit(t, at)
        t.cur = zone
    elseif not t.since then
        t.since = at
    end
    entry(t, zone)
    if sub then
        local sz = ns.Call("GetSubZoneText")
        if type(sz) == "string" and not ns.IsSecret(sz) and sz ~= "" then
            local full = zone .. " / " .. ns.Clean(sz, 60)
            if not has(t.subs, full) then t.subs[#t.subs + 1] = full end
        end
    end
    flush(c, t, key)
end

local function checkpoint()
    local c = ns.Character()
    if not c or not ns.Enabled("zones") then return end
    local day = today()
    local t, key = track(c, day)
    if not t.cur then return end
    credit(t, time())
    flush(c, t, key)
end

function ns.ZoneCount(field, n)
    if not ns.Enabled("zones") then return end
    n = tonumber(n)
    if not n or n <= 0 or n ~= n or n == math.huge then return end
    local c = ns.Character()
    if not c then return end
    local zone = zoneName()
    if not zone then return end
    local day = today()
    local t, key = track(c, day)
    local e = entry(t, zone)
    e[field] = (e[field] or 0) + n
    flush(c, t, key)
end

local function later()
    local at = time()
    if C_Timer and C_Timer.After then
        C_Timer.After(2, function() pcall(visit, true, at) end)
    else
        visit(true, at)
    end
end

ns.On("PLAYER_ENTERING_WORLD", function(isLogin, isReload)
    local c = ns.Character()
    if c and ns.db.openZone and ns.db.openZone[ns.StateKey(c)] then
        ns.db.openZone[ns.StateKey(c)].since = nil
    end
    later()
end, "Zones.world")
ns.On("ZONE_CHANGED_NEW_AREA", later, "Zones.zone")
ns.On("ZONE_CHANGED", function() pcall(visit, true) end, "Zones.sub")
ns.On("ZONE_CHANGED_INDOORS", function() pcall(visit, true) end, "Zones.subIndoors")

ns.On("PLAYER_LOGIN", function()
    if C_Timer and C_Timer.NewTicker then C_Timer.NewTicker(CHECKPOINT, function() pcall(checkpoint) end) end
end, "Zones.login")
ns.On("PLAYER_LOGOUT", function() pcall(checkpoint) end, "Zones.logout")

local lastXp, lastMax, lastLevel

local function readXp()
    local xp, max = tonumber((ns.Call("UnitXP", "player"))), tonumber((ns.Call("UnitXPMax", "player")))
    local level = tonumber((ns.Call("UnitLevel", "player")))
    return xp, max, level
end

local function baseline()
    lastXp, lastMax, lastLevel = readXp()
end

ns.On("PLAYER_XP_UPDATE", function(unit)
    if unit ~= nil and unit ~= "player" then return end
    if not ns.Enabled("zones") then return end
    local xp, max, level = readXp()
    if not xp then return end
    if not lastXp then
        lastXp, lastMax, lastLevel = xp, max, level
        return
    end
    local gain
    if level and lastLevel and level > lastLevel then
        gain = (lastMax or 0) - lastXp + xp
    elseif xp >= lastXp then
        gain = xp - lastXp
    elseif lastMax then
        gain = lastMax - lastXp + xp
    end
    lastXp, lastMax, lastLevel = xp, max or lastMax, level or lastLevel
    if gain and gain > 0 then ns.ZoneCount("xp", gain) end
end, "Zones.xp")
ns.On("PLAYER_LEVEL_UP", function(level)
    lastLevel = tonumber(level) or lastLevel
    if C_Timer and C_Timer.After then
        C_Timer.After(1, function() local x, m = readXp(); if x then lastXp, lastMax = x, m or lastMax end end)
    end
end, "Zones.level")
ns.On("PLAYER_ENTERING_WORLD", function() baseline() end, "Zones.xpBase")

ns.On("CHAT_MSG_COMBAT_XP_GAIN", function(text)
    if text == nil or ns.IsSecret(text) then return end
    local line = ns.ParseXPLine(text)
    if line and line.kind == "kill" then ns.ZoneCount("kills", 1) end
end, "Zones.kill")

ns.On("QUEST_TURNED_IN", function(_, xp)
    ns.ZoneCount("quests", 1)
    ns.ZoneCount("questXp", xp)
end, "Zones.quest")

local lastHk
local function todayHk()
    if not ns.Func("GetPVPSessionStats") then return nil end
    local hk = ns.Call("GetPVPSessionStats")
    if type(hk) == "number" and not ns.IsSecret(hk) then return hk end
end
ns.On("PLAYER_ENTERING_WORLD", function() lastHk = todayHk() end, "Zones.hkBase")
ns.On("PLAYER_PVP_KILLS_CHANGED", function()
    local hk = todayHk()
    if hk == nil then
        ns.ZoneCount("hks", 1)
    elseif lastHk == nil then
        lastHk = hk
    elseif hk > lastHk then
        ns.ZoneCount("hks", hk - lastHk)
        lastHk = hk
    elseif hk < lastHk then
        lastHk = hk
        if hk > 0 then ns.ZoneCount("hks", hk) end
    end
end, "Zones.hk")
