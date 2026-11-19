local _, ns = ...

local KINDS = { party = true, raid = true, pvp = true, arena = true }
local SLOTS = 19

local function groupSize()
    local n = ns.Call("GetNumGroupMembers") or 0
    return n > 0 and n or 1
end

local function here()
    local name, kind, difficultyId, difficulty, maxPlayers, _, _, instanceId = ns.Call("GetInstanceInfo")
    if not name or not KINDS[kind] then return nil end
    return {
        instanceId = instanceId,
        name = ns.Clean(name, 60),
        kind = kind,
        difficultyId = difficultyId,
        difficulty = ns.Clean(difficulty, 40),
        maxPlayers = maxPlayers,
    }
end

local function known(place)
    if (tonumber(place.difficultyId) or 0) == 0 then place.difficultyId = nil end
    if place.difficulty == "" then place.difficulty = nil end
    if (tonumber(place.maxPlayers) or 0) == 0 then place.maxPlayers = nil end
    return place
end

local function fillIn(place)
    local now = here()
    if not place or not now or now.instanceId ~= place.instanceId then return end
    known(now)
    for _, k in ipairs({ "difficultyId", "difficulty", "maxPlayers" }) do
        if place[k] == nil then place[k] = now[k] end
    end
end

local function gear()
    local out = {}
    for slot = 1, SLOTS do
        local id = tonumber((ns.Call("GetInventoryItemID", "player", slot)))
        if id then
            local e = { slot = slot, itemId = id }
            local link = ns.Call("GetInventoryItemLink", "player", slot)
            e.name, e.icon = ns.ItemNameIcon(id, link, slot)
            e.context = ns.ItemContext(link)
            out[#out + 1] = e
        end
    end
    return out
end

local function add(event, place, extra)
    local fields = ns.Copy(place)
    fields.at = time()
    fields.event = event
    fields.since, fields.logoutAt = nil, nil
    for k, v in pairs(extra or {}) do fields[k] = v end
    ns.AddHistory("instance", fields, event .. ":" .. tostring(place.instanceId) .. ":" .. ns.NextSeq())
    ns.Debug("instance " .. event .. " " .. tostring(place.name))
end

local function leave(key, was, at, reason)
    if not reason then fillIn(was) end
    local seconds = was.since and at - was.since or nil
    add("leave", was, { at = at, leaveAt = at, seconds = seconds, durationSeconds = seconds, reason = reason })
    ns.db.inside[key] = nil
end

local function check(isLogin, isReload, late)
    if not ns.Enabled("instance") then return end
    local c = ns.Character()
    if not c then return end
    ns.db.inside = ns.db.inside or {}
    local key = ns.StateKey(c)
    local was, now = ns.db.inside[key], here()
    if was and was.logoutAt then
        if isReload then
            was.logoutAt = nil
        else
            leave(key, was, was.logoutAt, "logout")
            was = nil
        end
    end
    if was and now and was.instanceId == now.instanceId then return end
    if was then leave(key, was, time()) end
    if now and not late and C_Timer and (now.kind == "party" or now.kind == "raid")
        and (tonumber(now.difficultyId) or 0) == 0 then
        C_Timer.After(3, function() check(isLogin, isReload, true) end)
        return
    end
    if now then
        known(now)
        local at = time()
        now.since = at
        now.enterAt = at
        now.visitId = "v-" .. ns.Hash(ns.Who(c) .. "|" .. at .. "|" .. tostring(now.instanceId))
        add("enter", now, { groupSize = groupSize(), gearAtEnter = gear() })
        ns.db.inside[key] = now
        if ns.OnVisit then ns.OnVisit(now.visitId) end
    end
end

function ns.CurrentVisitId()
    local c = ns.Character()
    local v = c and ns.db and ns.db.inside and ns.db.inside[ns.StateKey(c)]
    return v and not v.logoutAt and v.visitId or nil
end

ns.On("PLAYER_ENTERING_WORLD", check, "Instance.check")
ns.On("ZONE_CHANGED_NEW_AREA", function() check() end, "Instance.check")
ns.On("PLAYER_LOGOUT", function()
    local c = ns.Character()
    local v = c and ns.db.inside and ns.db.inside[ns.StateKey(c)]
    if v then v.logoutAt = time() end
end, "Instance.logout")

ns.On("ENCOUNTER_END", function(encounterId, encounterName, difficultyId, size, success)
    if not ns.Enabled("instance") or success ~= 1 and success ~= true then return end
    local c = ns.Character()
    local place = (c and ns.db.inside and ns.db.inside[ns.StateKey(c)]) or here() or { name = nil }
    fillIn(place)
    add("boss", place, {
        encounterId = tonumber(encounterId),
        encounter = ns.Clean(encounterName, 60),
        difficultyId = tonumber(difficultyId) or place.difficultyId,
        groupSize = tonumber(size) or groupSize(),
    })
end, "Instance.boss")
