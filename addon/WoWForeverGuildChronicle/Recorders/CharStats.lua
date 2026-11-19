local _, ns = ...

local KEEP_DAYS = 35

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

local function stats()
    if not ns.Enabled("charStats") then return end
    local c = ns.Character()
    if not c then return end
    local day = date("%Y-%m-%d", time())
    local f = { day = day, level = whole(ns.Call("UnitLevel", "player")), health = whole(ns.Call("UnitHealthMax", "player")) }
    f.power = whole(ns.Call("UnitPowerMax", "player"))
    local powerType = ns.Call("UnitPowerType", "player")
    if type(powerType) == "number" or (type(powerType) == "string" and not ns.IsSecret(powerType)) then f.powerType = ns.Clean(tostring(powerType), 20) end
    f.strength = whole(select(2, ns.Call("UnitStat", "player", 1)))
    f.agility = whole(select(2, ns.Call("UnitStat", "player", 2)))
    f.stamina = whole(select(2, ns.Call("UnitStat", "player", 3)))
    f.intellect = whole(select(2, ns.Call("UnitStat", "player", 4)))
    f.spirit = whole(select(2, ns.Call("UnitStat", "player", 5)))
    f.armor = whole(select(2, ns.Call("UnitArmor", "player")))
    local base, posBuff, negBuff = ns.Call("UnitAttackPower", "player")
    if num(base) then f.attackPower = whole((base or 0) + (posBuff or 0) + (negBuff or 0)) end
    f.spellPower = whole(ns.Call("GetSpellBonusDamage", 2))
    f.crit = pct(ns.Call("GetCritChance"))
    f.haste = pct(ns.Call("GetHaste"))
    f.mastery = pct(ns.Call("GetMastery"))
    f.dodge = pct(ns.Call("GetDodgeChance"))
    f.parry = pct(ns.Call("GetParryChance"))
    f.block = pct(ns.Call("GetBlockChance"))
    local any = false
    for k in pairs(f) do if k ~= "day" and k ~= "level" then any = true end end
    if not any then return end
    local key = ns.StateKey(c) .. "|" .. day
    ns.SetState("charStats", key, f)
    local cutoff = date("%Y-%m-%d", time() - KEEP_DAYS * 86400)
    for k, r in pairs(ns.db.state.charStats or {}) do
        if type(r) ~= "table" or type(r.day) ~= "string" or r.day < cutoff then ns.db.state.charStats[k] = nil end
    end
end

ns.ReadStats = stats
