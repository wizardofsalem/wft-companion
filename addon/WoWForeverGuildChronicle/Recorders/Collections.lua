local _, ns = ...

local function num(v)
    v = tonumber(v)
    if v and not ns.IsSecret(v) and v == v and v > -1e9 and v < 1e12 then return v end
end

local function whole(v)
    v = num(v)
    return v and math.floor(v) or nil
end

local function pct(v)
    v = num(v)
    return v and math.floor(v * 10 + 0.5) / 10 or nil
end

local function collections()
    if not ns.Enabled("collections") then return end
    local c = ns.Character()
    if not c then return end
    local f = {}
    if ns.Func("C_MountJournal.GetMountIDs") then
        local ids = {}
        for _, mountId in ipairs(ns.Call("C_MountJournal.GetMountIDs") or {}) do
            local r = { ns.Call("C_MountJournal.GetMountInfoByID", mountId) }
            if r[11] == true and #ids < 2000 then ids[#ids + 1] = whole(mountId) end
        end
        table.sort(ids)
        f.mounts = { count = #ids, ids = ids }
    end
    local titleCount = whole(ns.Call("GetNumTitles"))
    if titleCount and titleCount > 0 then
        local ids = {}
        for i = 1, math.min(titleCount, 500) do
            if ns.Call("IsTitleKnown", i) == true then ids[#ids + 1] = i end
        end
        local current = whole(ns.Call("GetCurrentTitle"))
        f.titles = { count = #ids, ids = ids, current = current and current > 0 and current or nil }
    end
    local points = whole(ns.Call("GetTotalAchievementPoints"))
    local done = whole((ns.Call("GetNumCompletedAchievements")))
    if points or done then f.achievements = { points = points, completed = done } end
    if next(f) == nil then return end
    ns.SetState("collections", ns.StateKey(c), f)
end

ns.ReadCollections = collections
