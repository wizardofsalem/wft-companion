local _, ns = ...

local KEEP = 30 * 86400
local MATCH = 10 * 60
local MIN_CHAT_QUALITY = 2
local MAX_ROLLS = 40
local MAX_RECENT = 200

local CHOICES = { NeedMainSpec = "need", NeedOffSpec = "needOffSpec", Transmog = "transmog", Greed = "greed",
    Pass = "pass", Disenchant = "disenchant" }
local DEFAULT_STATES = { [0] = "need", [1] = "needOffSpec", [2] = "transmog", [3] = "greed", [5] = "pass" }

local function choiceOf(state)
    if state == nil or ns.IsSecret(state) then return nil end
    local enum = type(_G.Enum) == "table" and _G.Enum.EncounterLootDropRollState
    if type(enum) == "table" then
        for k, v in pairs(enum) do
            if v == state then return CHOICES[k] end
        end
        return nil
    end
    return DEFAULT_STATES[state]
end

local function plainName(v)
    if v == nil or ns.IsSecret(v) then return nil end
    local member = ns.GroupMember(v)
    return member and member.name or ns.Clean(v, 64)
end

local function scope()
    local visit = ns.CurrentVisitId and ns.CurrentVisitId()
    if visit then return visit end
    local _, kind, _, _, _, _, _, instanceId = ns.Call("GetInstanceInfo")
    if kind == "party" or kind == "raid" then return "i" .. tostring(instanceId) end
    local session = ns.SessionId and ns.SessionId()
    if session then return "w" .. tostring(session) end
end

local function isWorld(s) return type(s) == "string" and s:sub(1, 1) == "w" end

local function drops(c)
    ns.db.lootDrops = ns.db.lootDrops or {}
    local s = scope()
    local key = isWorld(s) and (ns.StateKey(c) .. "#w") or ns.StateKey(c)
    local d = ns.db.lootDrops[key]
    if not d or (s and d.scope ~= s) then
        d = { scope = s, n = {}, recent = {} }
        ns.db.lootDrops[key] = d
    end
    return d
end

local function same(a, b)
    return ns.SamePlayer(a, b)
end

local function find(d, itemId, winner, lootListId, encounterId)
    local now = time()
    for i = #d.recent, 1, -1 do
        local r = d.recent[i]
        if now - r.seenAt <= MATCH and r.itemId == itemId then
            if lootListId and r.lootListId == lootListId and r.encounterId == encounterId then return r end
            if (not lootListId or not r.lootListId) and (not winner or not r.winner or same(r.winner, winner)) then return r end
        end
    end
end

local function prune(c)
    local bucket = ns.db.state.lootHistory
    if not bucket then return end
    local prefix, cutoff = ns.StateKey(c) .. "|", time() - KEEP
    for key, r in pairs(bucket) do
        if key:sub(1, #prefix) == prefix and (tonumber(r.at) or 0) < cutoff then bucket[key] = nil end
    end
end

local function note(seen)
    local c = ns.Character()
    if not c or not ns.Enabled("lootHistory") or not seen.itemId then return end
    local s = scope()
    if not s then return end
    local d = drops(c)
    local r = find(d, seen.itemId, seen.winner, seen.lootListId, seen.encounterId)
    if not r then
        d.n[seen.itemId] = (d.n[seen.itemId] or 0) + 1
        r = { itemId = seen.itemId, dropKey = s .. "|" .. seen.itemId .. "|" .. d.n[seen.itemId] }
        ns.Push(d.recent, r, MAX_RECENT)
    end
    r.seenAt = time()
    r.lootListId = r.lootListId or seen.lootListId
    r.encounterId = r.encounterId or seen.encounterId
    if seen.winner and (not r.winner or #seen.winner > #r.winner) then r.winner = seen.winner end
    r.quality = r.quality or seen.quality
    if seen.rolls then r.rolls = seen.rolls end
    ns.SetState("lootHistory", ns.StateKey(c) .. "|" .. r.dropKey, {
        dropKey = r.dropKey,
        encounterId = not isWorld(s) and r.encounterId or nil,
        visitId = ns.CurrentVisitId and ns.CurrentVisitId() or nil,
        itemId = r.itemId,
        quality = r.quality,
        rolls = r.rolls,
        winner = r.winner,
    })
    ns.Debug("loot history " .. r.dropKey .. (r.winner and " won" or ""))
end

local function quality(link, itemId)
    local q = select(3, ns.Call("GetItemInfo", link or itemId))
    return tonumber(q) or (link and ns.QualityFromLink(link)) or nil
end

local function readDrop(encounterId, lootListId)
    local lh = _G.C_LootHistory
    if type(lh) ~= "table" or type(lh.GetSortedInfoForDrop) ~= "function" then return end
    if ns.IsSecret(encounterId) or ns.IsSecret(lootListId) then return end
    local ok, info = pcall(lh.GetSortedInfoForDrop, encounterId, lootListId)
    if not ok or type(info) ~= "table" or ns.IsSecret(info) then return end
    local winner = type(info.winner) == "table" and plainName(info.winner.playerName) or nil
    if not winner and not info.allPassed then return end
    local link = not ns.IsSecret(info.itemHyperlink) and info.itemHyperlink or nil
    local itemId = link and ns.ItemIdFromLink(link)
    if not itemId then return end
    local rolls = {}
    for _, ri in ipairs(type(info.rollInfos) == "table" and info.rollInfos or {}) do
        local name = type(ri) == "table" and plainName(ri.playerName)
        local choice = name and choiceOf(ri.state)
        if choice and #rolls < MAX_ROLLS then
            local value = not ns.IsSecret(ri.roll) and tonumber(ri.roll) or nil
            rolls[#rolls + 1] = { name = name, choice = choice, value = value }
        end
    end
    note({ itemId = itemId, encounterId = tonumber(encounterId), lootListId = tonumber(lootListId),
        winner = winner, quality = quality(link, itemId), rolls = rolls })
end
ns.On("LOOT_HISTORY_UPDATE_DROP", readDrop, "LootHistory.drop")

ns.On("LOOT_HISTORY_UPDATE_ENCOUNTER", function(encounterId)
    local lh = _G.C_LootHistory
    if type(lh) ~= "table" or type(lh.GetSortedDropsForEncounter) ~= "function" or ns.IsSecret(encounterId) then return end
    local ok, list = pcall(lh.GetSortedDropsForEncounter, encounterId)
    if not ok or type(list) ~= "table" or ns.IsSecret(list) then return end
    for _, d in ipairs(list) do
        local key = type(d) == "table" and (d.lootListKey or d.lootListID)
        if key then readDrop(encounterId, key) end
    end
end, "LootHistory.encounter")

ns.On("ENCOUNTER_LOOT_RECEIVED", function(encounterId, itemId, link, _, playerName)
    itemId = tonumber(itemId) or (type(link) == "string" and ns.ItemIdFromLink(link)) or nil
    local winner = plainName(playerName)
    if not itemId or not winner then return end
    link = type(link) == "string" and not ns.IsSecret(link) and link or nil
    note({ itemId = itemId, encounterId = tonumber(encounterId), winner = winner, quality = quality(link, itemId) })
end, "LootHistory.encounterLoot")

ns.On("CHAT_MSG_LOOT", function(line)
    if type(line) ~= "string" or ns.IsSecret(line) or not ns.ParseOthersLoot then return end
    if (tonumber((ns.Call("GetNumGroupMembers"))) or 0) == 0 then return end
    local who, link = ns.ParseOthersLoot(line)
    if not who or who == ns.Call("UnitName", "player") then return end
    local itemId = ns.ItemIdFromLink(link)
    local q = itemId and quality(link, itemId)
    if not q or q < MIN_CHAT_QUALITY then return end
    note({ itemId = itemId, winner = plainName(who), quality = q })
end, "LootHistory.chat")

ns.On("PLAYER_ENTERING_WORLD", function(isLogin, isReload)
    local c = ns.Character()
    if c and ns.db and isLogin and not isReload then prune(c) end
end, "LootHistory.enter")
