local _, ns = ...

local SNAPSHOT_DELAY = 3
local BOSS_KILL_WINDOW = 120
local STALE = 30 * 60
local MAX_NAMES = 40

local bossKilledAt = {}

local function units()
    local out = { "player" }
    local n = tonumber((ns.Call("GetNumGroupMembers"))) or 0
    if n > 0 and ns.Call("IsInRaid") then
        out = {}
        for i = 1, math.min(n, 40) do out[#out + 1] = "raid" .. i end
    else
        for i = 1, math.min(math.max(n - 1, 0), 4) do out[#out + 1] = "party" .. i end
    end
    return out
end

local function snapshot()
    local alive, dead, selfAlive = {}, {}, nil
    for _, unit in ipairs(units()) do
        local p = ns.UnitPlayer(unit)
        local isDead = ns.Call("UnitIsDeadOrGhost", unit)
        if p and p.name and (isDead == true or isDead == false) then
            local list = isDead and dead or alive
            if #list < MAX_NAMES then list[#list + 1] = p.name end
            if unit == "player" or ns.Call("UnitIsUnit", unit, "player") then selfAlive = not isDead end
        end
    end
    table.sort(alive)
    table.sort(dead)
    return { alive = alive, dead = dead, selfAlive = selfAlive }
end

local function roles()
    local counts, mine = { tank = 0, healer = 0, damager = 0, none = 0 }, nil
    local any = false
    for _, unit in ipairs(units()) do
        local role = ns.Call("UnitGroupRolesAssigned", unit)
        if type(role) == "string" and not ns.IsSecret(role) then
            local key = role:lower()
            if counts[key] then
                counts[key] = counts[key] + 1
                any = true
                if unit == "player" or ns.Call("UnitIsUnit", unit, "player") == true then mine = key end
            end
        end
    end
    if not any then return nil, nil end
    return counts, mine
end

local function bossPct()
    local least
    for i = 1, 5 do
        local unit = "boss" .. i
        if ns.Call("UnitExists", unit) == true then
            local h, m = ns.Call("UnitHealth", unit), ns.Call("UnitHealthMax", unit)
            if type(h) == "number" and type(m) == "number" and not ns.IsSecret(h) and not ns.IsSecret(m) and m > 0 then
                local pct = math.max(0, math.min(100, math.floor(h * 100 / m + 0.5)))
                if not least or pct < least then least = pct end
            end
        end
    end
    return least
end

local function inInstance()
    local _, kind = ns.Call("GetInstanceInfo")
    return kind == "party" or kind == "raid" or kind == "pvp" or kind == "arena"
end

local function open(c)
    ns.db.openFight = ns.db.openFight or {}
    return ns.db.openFight[ns.StateKey(c)]
end

local function attempt(c, encounterId, scope)
    ns.db.fightAttempts = ns.db.fightAttempts or {}
    local key = ns.StateKey(c)
    local a = ns.db.fightAttempts[key]
    if not a or a.scope ~= scope then
        a = { scope = scope, n = {} }
        ns.db.fightAttempts[key] = a
    end
    a.n[encounterId] = (a.n[encounterId] or 0) + 1
    return a.n[encounterId]
end

local function outcome(f, snap)
    local everyoneDead = #snap.dead > 0 and #snap.alive == 0
    if f.success == true then return "kill", "encounterEnd" end
    if f.success == false then return everyoneDead and "wipe" or "reset", "encounterEnd" end
    local killed = bossKilledAt[f.encounterId]
    if killed and math.abs(killed - f.endAt) <= BOSS_KILL_WINDOW then return "kill", "bossKill" end
    if everyoneDead then return "wipe", "snapshot" end
    return "unknown", nil
end

local function write(c, f, snap)
    ns.db.openFight[ns.StateKey(c)] = nil
    if not ns.Enabled("fight") then return end
    local result, source = "unknown", nil
    if snap then result, source = outcome(f, snap) end
    local seconds = f.seconds or math.max(0, (f.endAt or time()) - f.start)
    ns.AddHistory("fight", {
        at = f.endAt or time(),
        encounterId = f.encounterId,
        encounterName = f.encounterName,
        instanceId = f.instanceId,
        visitId = f.visitId,
        difficultyId = f.difficultyId,
        difficulty = f.difficulty,
        start = f.start,
        seconds = math.min(seconds, 86400),
        outcome = result,
        outcomeSource = source,
        attempt = f.attempt,
        groupSize = f.groupSize,
        selfInInstance = f.selfInInstance,
        selfAlive = snap and snap.selfAlive,
        roles = f.roles,
        selfRole = f.selfRole,
        bossPct = f.bossPct,
        aliveAtEnd = snap and snap.alive,
        deadAtEnd = snap and snap.dead,
    }, tostring(f.encounterId) .. ":" .. f.start .. ":" .. tostring(f.attempt))
    ns.Debug("fight " .. tostring(f.encounterName) .. " " .. result)
end

local function closeStale(c, force)
    local f = open(c)
    if not f then return end
    if f.ended then
        write(c, f, f.snap)
    elseif force or time() - f.start > STALE then
        f.endAt = time()
        write(c, f, nil)
    end
end

ns.On("ENCOUNTER_START", function(encounterId, encounterName, difficultyId, groupSize)
    local c = ns.Character()
    if not c or not ns.Enabled("fight") then return end
    closeStale(c, true)
    encounterId = tonumber(encounterId)
    if not encounterId then return end
    local _, _, _, difficulty, _, _, _, instanceId = ns.Call("GetInstanceInfo")
    local visitId = ns.CurrentVisitId and ns.CurrentVisitId()
    local scope = visitId or ("s" .. tostring(ns.SessionId and ns.SessionId() or ""))
    local pullRoles, pullMine = roles()
    ns.db.openFight[ns.StateKey(c)] = {
        encounterId = encounterId,
        encounterName = ns.Clean(encounterName, 80),
        difficultyId = tonumber(difficultyId),
        difficulty = ns.Clean(difficulty, 40),
        instanceId = tonumber(instanceId) and instanceId > 0 and instanceId or nil,
        visitId = visitId,
        groupSize = tonumber(groupSize),
        roles = pullRoles,
        selfRole = pullMine,
        start = time(),
        startT = GetTime(),
        attempt = attempt(c, encounterId, scope),
    }
end, "Fight.start")

ns.On("BOSS_KILL", function(encounterId)
    encounterId = tonumber(encounterId)
    if encounterId then bossKilledAt[encounterId] = time() end
end, "Fight.bossKill")

ns.On("ENCOUNTER_END", function(encounterId, _, _, groupSize, success)
    local c = ns.Character()
    if not c or not ns.Enabled("fight") then return end
    local f = open(c)
    if not f or f.encounterId ~= tonumber(encounterId) then return end
    f.endAt = time()
    if f.startT and GetTime() >= f.startT then f.seconds = math.floor(GetTime() - f.startT + 0.5) end
    if success == 1 or success == true then
        f.success = true
    elseif success == 0 or success == false then
        f.success = false
    end
    f.groupSize = tonumber(groupSize) or f.groupSize
    if f.success == false then f.bossPct = bossPct() end
    f.selfInInstance = inInstance()
    f.ended = true
    f.snap = snapshot()
    C_Timer.After(SNAPSHOT_DELAY, function()
        local now = open(c)
        if now ~= f then return end
        local later = snapshot()
        if #later.alive + #later.dead > 0 then f.snap = later end
        write(c, f, f.snap)
    end)
end, "Fight.end")

ns.On("PLAYER_ENTERING_WORLD", function(isLogin, isReload)
    local c = ns.Character()
    if not c or not ns.db then return end
    ns.db.openFight = ns.db.openFight or {}
    if isLogin and not isReload then
        closeStale(c, true)
    else
        closeStale(c, false)
    end
end, "Fight.enter")
