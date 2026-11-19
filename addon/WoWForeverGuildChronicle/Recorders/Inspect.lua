local _, ns = ...

local SLOTS = 19
local HONOR_WINDOW = 60
local current

local function seenThisSession(guid)
    local session = ns.SessionId and ns.SessionId() or 0
    local d = ns.db.inspected
    if type(d) ~= "table" or d.session ~= session then
        d = { session = session, guids = {} }
        ns.db.inspected = d
    end
    if d.guids[guid] then return true end
    d.guids[guid] = true
    return false
end

local UNITS = { "target", "mouseover", "focus" }

local function unitFor(guid)
    for _, u in ipairs(UNITS) do
        if ns.Call("UnitGUID", u) == guid then return u end
    end
    for i = 1, 40 do
        local u = (ns.Call("IsInRaid") and "raid" or "party") .. i
        if ns.Call("UnitGUID", u) == guid then return u end
    end
end

ns.On("INSPECT_READY", function(guid)
    if not ns.Enabled("inspect") or not guid or guid == ns.Call("UnitGUID", "player") then return end
    local unit = unitFor(guid)
    local subject = unit and ns.UnitPlayer(unit)
    if not subject then return end
    local items = {}
    for slot = 1, SLOTS do
        local itemId = ns.ItemIdFromLink(ns.Call("GetInventoryItemLink", unit, slot))
        if itemId then items[#items + 1] = { slot = slot, itemId = itemId } end
    end
    if #items == 0 or seenThisSession(guid) then return end
    current = ns.AddHistory("inspect", { at = time(), subject = subject, items = items }, guid .. ":" .. ns.NextSeq())
    ns.Debug("inspect " .. subject.name .. ": " .. #items .. " items")
end, "Inspect.ready")

ns.On("INSPECT_HONOR_UPDATE", function()
    if not current or time() - current.at > HONOR_WINDOW then return end
    if ns.Func("HasInspectHonorData") and not ns.Call("HasInspectHonorData") then return end
    if not ns.Func("UnitPVPRank") then
        local todayHK, _, _, _, lifetimeHK = ns.Call("GetInspectHonorData")
        if not tonumber(lifetimeHK) then return end
        current.honor = { todayHK = tonumber(todayHK), lifetime = { hk = tonumber(lifetimeHK) } }
        return
    end
    local todayHK, _, yesterdayHK, yesterdayHonor, weekHK, weekHonor, lastWeekHK, lastWeekHonor,
        lastWeekStanding, lifetimeHK, _, highest = ns.Call("GetInspectHonorData")
    if not tonumber(lifetimeHK) then return end
    current.honor = {
        todayHK = tonumber(todayHK),
        yesterday = { hk = tonumber(yesterdayHK), honor = tonumber(yesterdayHonor) },
        thisWeek = { hk = tonumber(weekHK), honor = tonumber(weekHonor) },
        lastWeek = { hk = tonumber(lastWeekHK), honor = tonumber(lastWeekHonor), standing = tonumber(lastWeekStanding) },
        lifetime = { hk = tonumber(lifetimeHK), highestRank = tonumber(highest) and (highest >= 5 and highest - 4 or 0) },
    }
end, "Inspect.honor")
