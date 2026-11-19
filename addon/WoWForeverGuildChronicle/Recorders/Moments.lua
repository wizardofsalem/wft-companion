local _, ns = ...

local function moment(kind, fields)
    fields = fields or {}
    fields.kind = kind
    fields.at = time()
    for k, v in pairs(fields) do
        if type(v) == "number" then
            if v ~= v or v == math.huge or v == -math.huge then fields[k] = nil
            elseif v ~= math.floor(v) then fields[k] = math.floor(v) end
        end
    end
    return ns.AddHistory("moments", fields, kind .. ":" .. ns.NextSeq())
end
ns.Moment = moment

local function readable(v) return v ~= nil and not ns.IsSecret(v) end

local function hook(name, fn)
    if type(hooksecurefunc) ~= "function" then ns.NoteMissing("hooksecurefunc") return false end
    local space, member = name:match("^([%w_]+)%.([%w_]+)$")
    local ok
    if space then
        ok = type(_G[space]) == "table" and type(_G[space][member]) == "function"
            and pcall(hooksecurefunc, _G[space], member, fn)
    else
        ok = type(_G[name]) == "function" and pcall(hooksecurefunc, name, fn)
    end
    if not ok then ns.NoteMissing(name) end
    return ok and true or false
end
ns.HookGame = hook

local function safe(fn)
    return function(...)
        local ok, err = pcall(fn, ...)
        if not ok then ns.Log("error", "Moments: " .. tostring(err)) end
    end
end

local function itemIdOf(v)
    if type(v) == "number" then return v end
    if type(v) == "table" and type(v.GetItemID) == "function" then
        local ok, id = pcall(v.GetItemID, v)
        if ok then return id end
    end
    if type(v) == "string" then return ns.ItemIdFromLink(v) end
end


local function taxi(index)
    local name = type(TaxiNodeName) == "function" and select(2, pcall(TaxiNodeName, index)) or nil
    local cost = type(TaxiNodeCost) == "function" and select(2, pcall(TaxiNodeCost, index)) or nil
    ns.Mark("taxi")
    moment("taxi", { to = readable(name) and type(name) == "string" and ns.Clean(name, 80) or nil,
        cost = tonumber(cost) })
end
hook("TakeTaxiNode", safe(taxi))


local HEARTH_SPELLS = { [8690] = true, [556] = true }
ns.On("UNIT_SPELLCAST_SUCCEEDED", safe(function(unit, _, spellId)
    if unit ~= "player" or not readable(spellId) or not HEARTH_SPELLS[spellId] then return end
    moment("hearth", { spellId = spellId })
end), "Moments.cast")

local function bind()
    local where = ns.Call("GetBindLocation")
    moment("bind", { place = type(where) == "string" and ns.Clean(where, 60) or nil })
end
ns.On("HEARTHSTONE_BOUND", safe(bind), "Moments.bind")
ns.On("CONFIRM_BINDER", safe(function() ns.Mark("binder") end), "Moments.binder")

local resOffered
ns.On("RESURRECT_REQUEST", safe(function() resOffered = GetTime() end), "Moments.res")
local resHooked = hook("AcceptResurrect", safe(function() resOffered = nil; moment("resurrectAccepted") end))
local function resByEvent()
    if not resHooked and resOffered and GetTime() - resOffered < 60 then
        resOffered = nil
        moment("resurrectAccepted", { viaEvent = true })
    end
end
ns.On("PLAYER_ALIVE", safe(resByEvent), "Moments.alive")
ns.On("PLAYER_UNGHOST", safe(resByEvent), "Moments.unghost")

ns.On("CONFIRM_SUMMON", safe(function() ns.Mark("summonOffered") end), "Moments.summon")
if not hook("C_SummonInfo.ConfirmSummon", safe(function() moment("summonAccepted") end)) then
    hook("ConfirmSummon", safe(function() moment("summonAccepted") end))
end


ns.On("NEW_MOUNT_ADDED", safe(function(mountId)
    local name
    if C_MountJournal and C_MountJournal.GetMountInfoByID then
        local ok, n = pcall(C_MountJournal.GetMountInfoByID, mountId)
        if ok and type(n) == "string" and readable(n) then name = ns.Clean(n, 80) end
    end
    moment("mountLearned", { mountId = readable(mountId) and tonumber(mountId) or nil, name = name })
end), "Moments.mount")
ns.On("COMPANION_LEARNED", safe(function() moment("companionLearned") end), "Moments.companion")

local lastSpell
local function spellName(id)
    local n = ns.Call("C_Spell.GetSpellName", id)
    if type(n) == "string" and readable(n) then return ns.Clean(n, 80) end
end
local function learned(spellId)
    if not readable(spellId) or type(spellId) ~= "number" then return end
    local name = spellName(spellId)
    lastSpell = { name = name, t = GetTime() }
    moment("spellLearned", { spellId = spellId, name = name })
end
ns.On("LEARNED_SPELL_IN_SKILL_LINE", safe(learned), "Moments.learned")
ns.On("LEARNED_SPELL_IN_TAB", safe(learned), "Moments.learnedTab")

local LINES = {
    { "ERR_LEARN_SPELL_S", "spellLearned" }, { "ERR_LEARN_ABILITY_S", "spellLearned" },
    { "ERR_LEARN_PASSIVE_S", "spellLearned" }, { "ERR_LEARN_RECIPE_S", "recipeLearned" },
}
ns.On("CHAT_MSG_SYSTEM", safe(function(line)
    if type(line) ~= "string" or ns.IsSecret(line) then return end
    for _, l in ipairs(LINES) do
        local fmt = _G[l[1]]
        local args = fmt and ns.FormatArgs(fmt, line)
        if args and args[1] then
            local name = ns.Clean((args[1]:gsub("|c%x%x%x%x%x%x%x%x", ""):gsub("|r", ""):gsub("|H.-|h(.-)|h", "%1"):gsub("^%[(.*)%]$", "%1")), 80)
            if l[2] == "spellLearned" and lastSpell and lastSpell.name == name and GetTime() - lastSpell.t < 3 then return end
            moment(l[2], { name = name, fromLine = true })
            return
        end
    end
end), "Moments.learnedLine")


ns.On("CHAT_MSG_SKILL", safe(function(line)
    if type(line) ~= "string" or ns.IsSecret(line) then return end
    local fmt = _G.SKILL_RANK_UP
    local args = fmt and ns.FormatArgs(fmt, line)
    if not args or not args[1] then return end
    local level = tonumber(args[2])
    if not level then return end
    moment("skillGain", { name = ns.Clean((args[1]:gsub("|c%x%x%x%x%x%x%x%x", ""):gsub("|r", "")), 60), level = level })
end), "Moments.skillGain")

ns.On("PLAYER_SPECIALIZATION_CHANGED", safe(function(unit)
    if unit ~= nil and unit ~= "player" then return end
    local c = ns.Character()
    local s = ns.ActiveSpec and ns.ActiveSpec()
    if not c or not s then return end
    ns.db.lastSpec = ns.db.lastSpec or {}
    local key = ns.StateKey(c)
    local before = ns.db.lastSpec[key]
    ns.db.lastSpec[key] = s.specId
    if before and before ~= s.specId then moment("specChange", { specId = s.specId, specName = s.specName, role = s.role }) end
end), "Moments.specChange")


function ns.TradeDone(given, received, itemsGiven, itemsReceived)
    moment("tradeDone", { copperGiven = given, copperReceived = received, itemsGiven = itemsGiven, itemsReceived = itemsReceived })
end

ns.On("CHAT_MSG_SYSTEM", safe(function(line)
    if type(line) ~= "string" or ns.IsSecret(line) then return end
    for _, f in ipairs({ { "ERR_ZONE_EXPLORED_XP", true }, { "ERR_ZONE_EXPLORED", false } }) do
        local fmt = _G[f[1]]
        local args = fmt and ns.FormatArgs(fmt, line)
        if args and args[1] then
            local place = ns.Clean((args[1]:gsub("|c%x%x%x%x%x%x%x%x", ""):gsub("|r", "")), 60)
            moment("explored", { place = place, xp = f[2] and tonumber(args[2]) or nil })
            return
        end
    end
end), "Moments.explored")

ns.On("LFG_PROPOSAL_SHOW", safe(function()
    local r = { ns.Call("GetLFGProposal") }
    local name = type(r[5]) == "string" and not ns.IsSecret(r[5]) and ns.Clean(r[5], 80) or nil
    local id = tonumber(r[2])
    moment("lfgProposal", { name = name ~= "" and name or nil, dungeonId = id and not ns.IsSecret(id) and id > 0 and id or nil })
end), "Moments.lfgProposal")
ns.On("ACHIEVEMENT_EARNED", safe(function(achievementId)
    achievementId = tonumber(achievementId)
    if not achievementId or ns.IsSecret(achievementId) then return end
    local points = tonumber(select(3, ns.Call("GetAchievementInfo", achievementId)))
    moment("achievementEarned", { achievementId = achievementId, points = points and not ns.IsSecret(points) and points or nil })
end), "Moments.achievement")
ns.On("LFG_COMPLETION_REWARD", safe(function() moment("lfgDone", {}) end), "Moments.lfgDone")
ns.On("LFG_ROLE_CHECK_SHOW", safe(function() moment("lfgQueued", {}) end), "Moments.lfgQueued")


local function abandoned()
    local id
    if C_QuestLog and C_QuestLog.GetAbandonQuest then
        local ok, v = pcall(C_QuestLog.GetAbandonQuest)
        if ok then id = v end
    elseif type(GetAbandonQuest) == "function" then
        local ok, v = pcall(GetAbandonQuest)
        if ok then id = v end
    end
    local name
    if type(GetAbandonQuestName) == "function" then
        local ok, v = pcall(GetAbandonQuestName)
        if ok and type(v) == "string" and readable(v) then name = ns.Clean(v, 80) end
    end
    moment("questAbandoned", { questId = readable(id) and tonumber(id) or nil, name = name })
end
if not hook("C_QuestLog.AbandonQuest", safe(abandoned)) then hook("AbandonQuest", safe(abandoned)) end


local function used(bag, slot)
    local id
    if C_Container and C_Container.GetContainerItemID then
        local ok, v = pcall(C_Container.GetContainerItemID, bag, slot)
        if ok then id = v end
    end
    if not readable(id) or type(id) ~= "number" then return end
    local classId = select(6, ns.Call("GetItemInfoInstant", id))
    if classId == nil and C_Item and C_Item.GetItemInfoInstant then
        local ok, _, _, _, _, _, c = pcall(C_Item.GetItemInfoInstant, id)
        if ok then classId = c end
    end
    if tonumber(classId) ~= 0 then return end
    moment("consumable", { itemId = id })
end
if not hook("C_Container.UseContainerItem", safe(used)) then hook("UseContainerItem", safe(used)) end


local function auction(kind, fields)
    return safe(function(...)
        local f = fields and fields(...) or {}
        moment(kind, f)
    end)
end
local AH = "C_AuctionHouse."
hook(AH .. "PostItem", auction("auctionPost", function(item, duration, quantity, bid, buyout)
    return { itemId = itemIdOf(item), quantity = tonumber(quantity), bid = tonumber(bid), buyout = tonumber(buyout) }
end))
hook(AH .. "PostCommodity", auction("auctionPost", function(item, duration, quantity, unitPrice)
    return { itemId = itemIdOf(item), quantity = tonumber(quantity), unitPrice = tonumber(unitPrice) }
end))
hook(AH .. "PlaceBid", auction("auctionBid", function(_, amount) return { amount = tonumber(amount) } end))
hook(AH .. "ConfirmCommoditiesPurchase", auction("auctionBuy", function(itemId, quantity)
    return { itemId = tonumber(itemId), quantity = tonumber(quantity) }
end))
hook(AH .. "CancelAuction", auction("auctionCancel"))
hook("PlaceAuctionBid", auction("auctionBid", function(_, _, amount) return { amount = tonumber(amount) } end))
hook("StartAuction", auction("auctionPost", function(minBid, buyout) return { bid = tonumber(minBid), buyout = tonumber(buyout) } end))
hook("CancelAuction", auction("auctionCancel"))

local AH_LINES = {
    { "ERR_AUCTION_SOLD_S", "auctionSold" }, { "ERR_AUCTION_WON_S", "auctionWon" },
    { "ERR_AUCTION_EXPIRED_S", "auctionExpired" }, { "ERR_AUCTION_OUTBID_S", "auctionOutbid" },
    { "ERR_AUCTION_REMOVED_S", "auctionRemoved" }, { "ERR_AUCTION_STARTED", "auctionStarted" },
    { "ERR_AUCTION_BID_PLACED", "auctionBidPlaced" },
}
ns.On("CHAT_MSG_SYSTEM", safe(function(line)
    if type(line) ~= "string" or ns.IsSecret(line) then return end
    for _, l in ipairs(AH_LINES) do
        local fmt = _G[l[1]]
        if fmt then
            local args = ns.FormatArgs(fmt, line)
            if args then
                moment(l[2], { name = args[1] and ns.Clean(args[1], 80) or nil })
                return
            elseif fmt == line then
                moment(l[2])
                return
            end
        end
    end
end), "Moments.auctionLine")


ns.On("CHAT_MSG_COMBAT_FACTION_CHANGE", safe(function(line)
    if type(line) ~= "string" or ns.IsSecret(line) then return end
    for _, l in ipairs({ { "FACTION_STANDING_INCREASED", 1 }, { "FACTION_STANDING_DECREASED", -1 } }) do
        local fmt = _G[l[1]]
        local args = fmt and ns.FormatArgs(fmt, line)
        if args and args[1] and tonumber(args[2]) then
            moment("repGain", { faction = ns.Clean(args[1], 80), amount = l[2] * tonumber(args[2]) })
            return
        end
    end
end), "Moments.rep")

ns.On("CHAT_MSG_COMBAT_HONOR_GAIN", safe(function(line)
    if type(line) ~= "string" or ns.IsSecret(line) then return end
    local fmt = _G.COMBATLOG_HONORGAIN
    local args = fmt and ns.FormatArgs(fmt, line)
    if args and tonumber(args[3]) then
        moment("honorGain", { victim = ns.Clean(args[1], 60), rank = ns.Clean(args[2], 40), amount = tonumber(args[3]) })
        return
    end
    fmt = _G.COMBATLOG_HONORGAIN_NO_RANK
    args = fmt and ns.FormatArgs(fmt, line)
    if args and tonumber(args[2]) then
        moment("honorGain", { victim = ns.Clean(args[1], 60), amount = tonumber(args[2]) })
        return
    end
    fmt = _G.COMBATLOG_HONORAWARD
    args = fmt and ns.FormatArgs(fmt, line)
    if args and tonumber(args[1]) then moment("honorAward", { amount = tonumber(args[1]) }) end
end), "Moments.honor")


local broken = {}
local pendingDur = false
local function durability(why)
    local lowest, sum, n, brokenNow = nil, 0, 0, 0
    for slot = 1, 19 do
        local cur, max = ns.Call("GetInventoryItemDurability", slot)
        if type(cur) == "number" and type(max) == "number" and max > 0 then
            local pct = cur / max * 100
            lowest = lowest and math.min(lowest, pct) or pct
            sum, n = sum + pct, n + 1
            if cur == 0 then
                brokenNow = brokenNow + 1
                if not broken[slot] then
                    broken[slot] = true
                    moment("gearBroken", { slot = slot, itemId = ns.ItemIdFromLink(ns.Call("GetInventoryItemLink", "player", slot)) })
                end
            else
                broken[slot] = nil
            end
        end
    end
    if why == "logout" and n > 0 then
        moment("durability", { lowestPct = math.floor(lowest + 0.5), averagePct = math.floor(sum / n + 0.5),
            brokenCount = brokenNow })
    end
end
ns.On("UPDATE_INVENTORY_DURABILITY", safe(function()
    if pendingDur or not C_Timer or not C_Timer.After then return end
    pendingDur = true
    C_Timer.After(5, function() pendingDur = false; pcall(durability, "change") end)
end), "Moments.durability")
ns.On("PLAYER_LOGOUT", safe(function() durability("logout") end), "Moments.durabilityLogout")


local lastRoles, rolesPending = nil, false
local function roles(why)
    local members = {}
    local selfRole = ns.Call("UnitGroupRolesAssigned", "player")
    local n = tonumber((ns.Call("GetNumGroupMembers"))) or 0
    if n < 2 then lastRoles = nil return end
    local counts = { TANK = 0, HEALER = 0, DAMAGER = 0, NONE = 0 }
    local function add(unit)
        local role = ns.Call("UnitGroupRolesAssigned", unit)
        if not readable(role) or type(role) ~= "string" then return end
        counts[role] = (counts[role] or 0) + 1
        local p = unit ~= "player" and ns.UnitPlayer(unit)
        members[#members + 1] = { name = p and p.name or nil, role = role, self = unit == "player" or nil }
    end
    add("player")
    local raid = ns.Call("IsInRaid")
    for i = 1, math.min(n - (raid and 0 or 1), 40) do add((raid and "raid" or "party") .. i) end
    local key = ns.Serialize(members)
    if key == lastRoles then return end
    lastRoles = key
    moment("roles", { why = why, selfRole = readable(selfRole) and selfRole or nil, tank = counts.TANK,
        healer = counts.HEALER, damager = counts.DAMAGER, none = counts.NONE, members = members })
end
local function rolesLater(why)
    return safe(function()
        if why == "encounter" or not C_Timer or not C_Timer.After then return roles(why) end
        if rolesPending then return end
        rolesPending = true
        C_Timer.After(5, function() rolesPending = false; pcall(roles, why) end)
    end)
end
ns.On("GROUP_ROSTER_UPDATE", rolesLater("group"), "Moments.rolesGroup")
ns.On("ROLE_CHANGED_INFORM", rolesLater("role"), "Moments.rolesChanged")
ns.On("ENCOUNTER_START", rolesLater("encounter"), "Moments.rolesEncounter")
