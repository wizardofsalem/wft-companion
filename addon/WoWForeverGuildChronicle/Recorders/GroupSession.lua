local _, ns = ...

local KEEP = 30 * 86400
local MAX_MEMBERS = 80

local function units()
    local out = { "player" }
    local n = tonumber((ns.Call("GetNumGroupMembers"))) or 0
    if n > 0 and ns.Call("IsInRaid") then
        out = {}
        for i = 1, math.min(n, 40) do out[#out + 1] = "raid" .. i end
    else
        for i = 1, math.min(math.max(n - 1, 0), 4) do out[#out + 1] = "party" .. i end
    end
    return out, n
end

local function shape(g)
    local parts = { g.kind, g.maxSize, g["end"] or "", #g.visits }
    for _, m in ipairs(g.members) do
        parts[#parts + 1] = table.concat({ m.name, m.joinedAt, m.leftAt or "", m.class and 1 or 0,
            m.race and 1 or 0, m.guildName and 1 or 0 }, ":")
    end
    return table.concat(parts, "|")
end

local function write(c, g)
    if not ns.Enabled("groupSession") then return end
    local now = shape(g)
    if g.written == now then return end
    g.written = now
    local members = {}
    for _, m in ipairs(g.members) do members[#members + 1] = ns.Copy(m) end
    ns.SetState("groupSession", ns.StateKey(c) .. "|" .. g.start, {
        sessionId = "gs-" .. ns.Hash(ns.Who(c) .. "|" .. g.start),
        start = g.start, ["end"] = g["end"], kind = g.kind, maxSize = g.maxSize,
        members = members, visits = ns.Copy(g.visits),
    })
end

local function prune(c)
    local bucket = ns.db.state.groupSession
    if not bucket then return end
    local prefix, cutoff = ns.StateKey(c) .. "|", time() - KEEP
    for key, r in pairs(bucket) do
        if key:sub(1, #prefix) == prefix and (tonumber(r.start) or 0) < cutoff then bucket[key] = nil end
    end
end

local function close(c, g, at)
    g["end"] = at
    for _, m in ipairs(g.members) do m.leftAt = m.leftAt or at end
    write(c, g)
    ns.db.openGroup[ns.StateKey(c)] = nil
end

local function update()
    local c = ns.Character()
    if not c or not ns.db then return end
    ns.db.openGroup = ns.db.openGroup or {}
    local key = ns.StateKey(c)
    local g = ns.db.openGroup[key]
    local list, n = units()
    local now = time()
    if n == 0 then
        if g then close(c, g, now) end
        return
    end
    if not g then
        g = { start = now, kind = ns.Call("IsInRaid") and "raid" or "party", maxSize = 0, members = {}, visits = {} }
        local visit = ns.CurrentVisitId and ns.CurrentVisitId()
        if visit then g.visits[1] = visit end
        ns.db.openGroup[key] = g
    end
    if ns.Call("IsInRaid") then g.kind = "raid" end
    g.maxSize = math.max(g.maxSize, n)
    g.changedAt = now
    local byName, present = {}, {}
    for _, m in ipairs(g.members) do byName[m.name] = m end
    for _, unit in ipairs(list) do
        local p = ns.UnitPlayer(unit)
        if p and p.name then
            local m = byName[p.name]
            if not m and #g.members < MAX_MEMBERS then
                m = { name = p.name, joinedAt = now, self = unit == "player" or ns.Call("UnitIsUnit", unit, "player") or nil }
                g.members[#g.members + 1] = m
                byName[p.name] = m
            end
            if m then
                m.class = p.class or m.class
                m.level = p.level or m.level
                m.race = p.race or m.race
                m.guildName = p.guild or m.guildName
                m.leftAt = nil
                present[p.name] = true
            end
        end
    end
    for _, m in ipairs(g.members) do
        if not present[m.name] and not m.leftAt then m.leftAt = now end
    end
    write(c, g)
end

function ns.OnVisit(visitId)
    local c = ns.Character()
    local g = c and ns.db.openGroup and ns.db.openGroup[ns.StateKey(c)]
    if not g or not visitId then return end
    for _, v in ipairs(g.visits) do if v == visitId then return end end
    g.visits[#g.visits + 1] = visitId
    write(c, g)
end

ns.On("GROUP_ROSTER_UPDATE", update, "GroupSession.update")
ns.On("PLAYER_ENTERING_WORLD", function(isLogin, isReload)
    local c = ns.Character()
    if not c then return end
    ns.db.openGroup = ns.db.openGroup or {}
    local g = ns.db.openGroup[ns.StateKey(c)]
    if isLogin and not isReload then
        if g then close(c, g, g.changedAt or g.start) end
        prune(c)
    end
    update()
end, "GroupSession.enter")
