local _, ns = ...

local KEEP_DAYS = 35
local lastDay

local function prune()
    local bucket = ns.db.state.dayCounts
    if not bucket then return end
    local cutoff = date("%Y-%m-%d", time() - KEEP_DAYS * 86400)
    for key, r in pairs(bucket) do
        if type(r) ~= "table" or type(r.day) ~= "string" or r.day < cutoff then bucket[key] = nil end
    end
end

function ns.DayCount(field, n)
    if not ns.Enabled("dayCounts") then return end
    local c = ns.Character()
    if not c then return end
    local day = date("%Y-%m-%d", time())
    if day ~= lastDay then
        prune()
        lastDay = day
    end
    local key = ns.StateKey(c) .. "|" .. day
    local old = (ns.db.state.dayCounts or {})[key]
    local fields = { day = day }
    if old then
        for k, v in pairs(old) do
            if type(v) == "number" and k ~= "at" then fields[k] = v end
        end
    end
    fields[field] = (fields[field] or 0) + (n or 1)
    ns.SetState("dayCounts", key, fields)
end
