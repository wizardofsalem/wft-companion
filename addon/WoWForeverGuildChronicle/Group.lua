local _, ns = ...

local function plain(v)
    if v == nil or ns.IsSecret(v) then return nil end
    return v
end

local surnames
function ns.Surnames()
    if surnames == nil then
        local c = ns.Character()
        if not c then return false end
        surnames = c.surname ~= nil
    end
    return surnames
end

function ns.SamePlayer(a, b)
    if type(a) ~= "string" or type(b) ~= "string" then return false end
    a, b = a:gsub("-", " "):lower(), b:gsub("-", " "):lower()
    if a == b then return true end
    local fa, fb = a:match("^(%S+)"), b:match("^(%S+)")
    return fa == fb and (not a:find(" ") or not b:find(" "))
end

local function unitInfo(unit)
    local name, second = ns.Call("UnitName", unit)
    name, second = plain(name), plain(second)
    if not name or name == "" then return nil end
    if second and second ~= "" then
        if ns.Surnames() then
            name = name .. " " .. second
        else
            name = name .. "-" .. second
        end
    end
    local _, classFile = ns.Call("UnitClass", unit)
    local level = plain(ns.Call("UnitLevel", unit))
    return {
        name = ns.Clean(name, 60),
        guid = plain(ns.Call("UnitGUID", unit)),
        class = plain(classFile),
        level = tonumber(level) and level > 0 and level or nil,
    }
end

function ns.GroupMembers()
    local out = {}
    local n = tonumber((ns.Call("GetNumGroupMembers"))) or 0
    if n > 0 and ns.Call("IsInRaid") then
        for i = 1, n do out[#out + 1] = unitInfo("raid" .. i) end
    else
        out[#out + 1] = unitInfo("player")
        for i = 1, math.min(n - 1, 4) do out[#out + 1] = unitInfo("party" .. i) end
    end
    table.sort(out, function(a, b) return a.name < b.name end)
    return out
end

function ns.GroupMember(name)
    if not name then return nil end
    for _, m in ipairs(ns.GroupMembers()) do
        if ns.SamePlayer(m.name, name) then return m end
    end
end

function ns.UnitPlayer(unit)
    if not ns.Call("UnitIsPlayer", unit) then return nil end
    local p = unitInfo(unit)
    if not p then return nil end
    local guild, rankName = ns.Call("GetGuildInfo", unit)
    if plain(guild) and guild ~= "" then
        p.guild = ns.Clean(guild, 60)
        p.guildRank = plain(rankName) and ns.Clean(rankName, 40) or nil
    end
    local _, race = ns.Call("UnitRace", unit)
    p.race = plain(race)
    local faction = plain(ns.Call("UnitFactionGroup", unit))
    p.faction = (faction == "Horde" or faction == "Alliance") and faction:lower() or nil
    local rank = tonumber(plain(ns.Call("UnitPVPRank", unit)))
    p.pvpRank = rank and (rank >= 5 and rank - 4 or 0) or nil
    return p
end
