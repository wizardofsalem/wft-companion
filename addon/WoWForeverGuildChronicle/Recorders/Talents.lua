local _, ns = ...

local function treeName(tab)
    local r = { ns.Call("GetTalentTabInfo", tab) }
    if type(r[1]) == "number" then return r[2] end
    return r[1]
end

local function spellName(id)
    if not id then return nil end
    if ns.Func("C_Spell.GetSpellName") then return ns.Call("C_Spell.GetSpellName", id) end
    return (ns.Call("GetSpellInfo", id))
end

local function specCall(name, ...)
    local fn = ns.Func("C_SpecializationInfo." .. name) and "C_SpecializationInfo." .. name
        or ns.Func(name) and name
    if not fn then return nil end
    return ns.Call(fn, ...)
end

local function spec()
    local active = tonumber((specCall("GetSpecialization")))
    if not active or ns.IsSecret(active) then return nil end
    local id, name, _, _, role = specCall("GetSpecializationInfo", active)
    id = tonumber(id)
    if not id or ns.IsSecret(id) then return nil end
    local out = { specId = id }
    if type(name) == "string" and not ns.IsSecret(name) and name ~= "" then out.specName = ns.Clean(name, 40) end
    if type(role) == "string" and not ns.IsSecret(role) and role ~= "" then out.role = ns.Clean(role, 10) end
    return out
end

local function index(values)
    local list, seen = {}, {}
    for _, v in ipairs(values) do
        if not seen[v] then seen[v] = true; list[#list + 1] = v end
    end
    table.sort(list)
    local at = {}
    for i, v in ipairs(list) do at[v] = i end
    return at
end

local function retailTrees()
    for _, f in ipairs({ "C_ClassTalents.GetActiveConfigID", "C_Traits.GetConfigInfo", "C_Traits.GetTreeNodes",
        "C_Traits.GetNodeInfo", "C_Traits.GetEntryInfo", "C_Traits.GetDefinitionInfo" }) do
        if not ns.Func(f) then return nil end
    end
    local config = ns.Call("C_ClassTalents.GetActiveConfigID")
    if not config then ns.Debug("talents: no active loadout (config nil)"); return {}, nil end
    local info = ns.Call("C_Traits.GetConfigInfo", config)
    if type(info) ~= "table" or type(info.treeIDs) ~= "table" then
        ns.Debug("talents: loadout " .. tostring(config) .. " has no trees")
        return {}, nil
    end
    local seenNodes, seenBought = 0, 0
    local current = spec()
    local specName = current and current.specName
    local className = ns.Call("UnitClass", "player")
    local trees, unspent = {}, nil
    for _, treeId in ipairs(info.treeIDs) do
        local groups, order = {}, {}
        local xs, ys = {}, {}
        local nodes = ns.Call("C_Traits.GetTreeNodes", treeId)
        for _, nodeId in ipairs(type(nodes) == "table" and nodes or {}) do
            local n = ns.Call("C_Traits.GetNodeInfo", config, nodeId)
            local bought = type(n) == "table" and (tonumber(n.ranksPurchased) or tonumber(n.activeRank)) or 0
            seenNodes = seenNodes + 1
            if bought > 0 then seenBought = seenBought + 1 end
            if bought > 0 and n.isVisible ~= false and type(n.activeEntry) == "table" then
                local entry = ns.Call("C_Traits.GetEntryInfo", config, n.activeEntry.entryID)
                local def = type(entry) == "table" and ns.Call("C_Traits.GetDefinitionInfo", entry.definitionID)
                local name = type(def) == "table" and (def.overrideName ~= "" and def.overrideName or spellName(def.spellID))
                if type(name) == "string" and name ~= "" then
                    local sub = tonumber(n.subTreeID) or 0
                    if not groups[sub] then groups[sub] = {}; order[#order + 1] = sub end
                    local x, y = tonumber(n.posX) or 0, tonumber(n.posY) or 0
                    xs[#xs + 1], ys[#ys + 1] = x, y
                    table.insert(groups[sub], { name = name, x = x, y = y, rank = bought, maxRank = tonumber(n.maxRanks) })
                end
            end
        end
        local col, row = index(xs), index(ys)
        for _, sub in ipairs(order) do
            local tree = { points = 0, talents = {} }
            if sub ~= 0 and ns.Func("C_Traits.GetSubTreeInfo") then
                local st = ns.Call("C_Traits.GetSubTreeInfo", config, sub)
                tree.name = type(st) == "table" and st.name or nil
            end
            tree.name = ns.Clean(tree.name or specName or className or "Talents", 40)
            for _, t in ipairs(groups[sub]) do
                tree.points = tree.points + t.rank
                if #tree.talents < 100 then
                    tree.talents[#tree.talents + 1] = { name = ns.Clean(t.name, 60),
                        tier = math.min(row[t.y], 20), column = math.min(col[t.x], 10),
                        rank = math.min(t.rank, 10), maxRank = t.maxRank and math.min(t.maxRank, 10) or nil }
                end
            end
            tree.points = math.min(tree.points, 200)
            if #trees < 10 then trees[#trees + 1] = tree end
        end
        if ns.Func("C_Traits.GetTreeCurrencyInfo") then
            local cur = ns.Call("C_Traits.GetTreeCurrencyInfo", config, treeId, false)
            for _, cu in ipairs(type(cur) == "table" and cur or {}) do
                local q = type(cu) == "table" and tonumber(cu.quantity)
                if q then unspent = math.min((unspent or 0) + q, 200) end
            end
        end
    end
    ns.Debug("talents: loadout " .. tostring(config) .. ", " .. #info.treeIDs .. " trees, " .. seenNodes
        .. " nodes, " .. seenBought .. " bought, unspent " .. tostring(unspent))
    return trees, unspent
end

local function capture(why)
    if not ns.Enabled("talents") then return end
    local c = ns.Character()
    if not c then return end
    if not ns.Func("GetTalentInfo") then
        local trees, unspent = retailTrees()
        if trees == nil then ns.NoteMissing("GetTalentInfo"); return end
        if #trees == 0 then
            ns.Debug("talents (" .. why .. "): no talents bought yet")
            return
        end
        ns.Debug("talents (" .. why .. "): " .. #trees .. " trees (C_Traits)")
        local fields = { trees = trees, unspent = unspent }
        for k, v in pairs(spec() or {}) do fields[k] = v end
        ns.SetState("talents", ns.StateKey(c), fields)
        return
    end
    local tabs = ns.Call("GetNumTalentTabs") or 0
    if tabs == 0 then
        ns.Debug("talents (" .. why .. "): no talent trees")
        return
    end
    local trees = {}
    for tab = 1, tabs do
        local tree = { name = ns.Clean(treeName(tab), 40), points = 0, talents = {} }
        for i = 1, ns.Call("GetNumTalents", tab) or 0 do
            local name, _, tier, column, rank, maxRank = ns.Call("GetTalentInfo", tab, i)
            if name and (rank or 0) > 0 then
                tree.points = tree.points + rank
                tree.talents[#tree.talents + 1] = {
                    name = ns.Clean(name, 60), tier = tier, column = column, rank = rank, maxRank = maxRank,
                }
            end
        end
        trees[#trees + 1] = tree
    end
    ns.Debug("talents (" .. why .. "): " .. #trees .. " trees")
    ns.SetState("talents", ns.StateKey(c), { trees = trees, unspent = ns.Call("UnitCharacterPoints", "player") })
end

ns.On("PLAYER_LOGIN", function()
    if C_Timer then C_Timer.After(5, function() capture("login") end) end
end, "Talents.login")
ns.ActiveSpec = spec
ns.On("PLAYER_SPECIALIZATION_CHANGED", function(unit)
    if unit == nil or unit == "player" then capture("specialization changed") end
end, "Talents.spec")
ns.On("CHARACTER_POINTS_CHANGED", function() capture("points changed") end, "Talents.changed")
ns.On("PLAYER_TALENT_UPDATE", function() capture("talent update") end, "Talents.update")
local pending = false
ns.On("TRAIT_CONFIG_UPDATED", function()
    if not C_Timer then return capture("loadout changed") end
    if pending then return end
    pending = true
    C_Timer.After(1, function() pending = false; capture("loadout changed") end)
end, "Talents.traits")
