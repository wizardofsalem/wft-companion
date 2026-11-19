local _, ns = ...
ns = ns or {}

local TWO32 = 4294967296

function ns.Hash(s)
    local a, b = 5381, 0
    for i = 1, #s do
        local c = s:byte(i)
        a = (a * 33 + c) % TWO32
        b = (c + b * 65599) % TWO32
    end
    return string.format("%08x%08x", a, b)
end

local function keyOrder(x, y)
    local tx, ty = type(x), type(y)
    if tx ~= ty then return tx < ty end
    if tx == "number" or tx == "string" then return x < y end
    return tostring(x) < tostring(y)
end

function ns.Serialize(v)
    local t = type(v)
    if t == "table" then
        local keys = {}
        for k in pairs(v) do keys[#keys + 1] = k end
        table.sort(keys, keyOrder)
        local parts = {}
        for _, k in ipairs(keys) do
            parts[#parts + 1] = ns.Serialize(k) .. "=" .. ns.Serialize(v[k])
        end
        return "{" .. table.concat(parts, ",") .. "}"
    elseif t == "string" then
        return string.format("%q", v)
    end
    return tostring(v)
end

function ns.Push(list, item, limit)
    list[#list + 1] = item
    while limit and #list > limit do
        table.remove(list, 1)
    end
end

function ns.PruneOlder(list, cutoff)
    local kept, removed = {}, 0
    for _, r in ipairs(list) do
        if type(r) == "table" and (r.at or 0) >= cutoff then
            kept[#kept + 1] = r
        else
            removed = removed + 1
        end
    end
    for i = #list, 1, -1 do list[i] = nil end
    for i, r in ipairs(kept) do list[i] = r end
    return removed
end

function ns.ItemIdFromLink(link)
    if type(link) ~= "string" then return nil end
    return tonumber(link:match("item:(%d+)"))
end

function ns.ItemContext(link)
    if type(link) ~= "string" or ns.IsSecret(link) then return nil end
    local body = link:match("item:([^|]*)")
    if not body then return nil end
    local n = 0
    for field in (body .. ":"):gmatch("([^:]*):") do
        n = n + 1
        if n == 12 then
            local v = tonumber(field)
            if v and v > 0 and v < 1e6 then return math.floor(v) end
            return nil
        end
    end
end

function ns.SpellIdFromLink(link)
    if type(link) ~= "string" then return nil end
    return tonumber(link:match("enchant:(%d+)") or link:match("spell:(%d+)"))
end

function ns.ParseGUID(guid)
    if type(guid) ~= "string" or guid == "" then return "unknown", nil end
    local parts = {}
    for p in guid:gmatch("[^-]+") do parts[#parts + 1] = p end
    local unitType = parts[1]
    if unitType == "Creature" or unitType == "Vehicle" then
        return "creature", tonumber(parts[6])
    elseif unitType == "Pet" then
        return "pet", tonumber(parts[6])
    elseif unitType == "GameObject" then
        return "object", tonumber(parts[6])
    elseif unitType == "Item" then
        return "item", nil
    elseif unitType == "Player" then
        return "player", nil
    end
    return "unknown", nil
end

function ns.Clean(s, maxLen)
    if s == nil or (ns.IsSecret and ns.IsSecret(s)) then return nil end
    s = tostring(s)
    s = s:gsub("|c%x%x%x%x%x%x%x%x", ""):gsub("|r", "")
    s = s:gsub("%c", "")
    maxLen = maxLen or 100
    if #s > maxLen then
        local cut = maxLen
        while cut > 0 do
            local nb = s:byte(cut + 1)
            if not nb or nb < 128 or nb >= 192 then break end
            cut = cut - 1
        end
        s = s:sub(1, cut)
    end
    return s
end

function ns.FormatToPattern(fmt)
    if type(fmt) ~= "string" or fmt == "" then return nil end
    local p = fmt:gsub("%%%d+%$([sd])", "%%%1")
    p = p:gsub("[%^%$%(%)%.%[%]%*%+%-%?%%]", "%%%0")
    p = p:gsub("%%%%s", "(.+)"):gsub("%%%%d", "(%%d+)")
    return "^" .. p .. "$"
end

function ns.FormatArgs(fmt, line)
    local pattern = ns.FormatToPattern(fmt)
    if not pattern or type(line) ~= "string" then return nil end
    local caps = { line:match(pattern) }
    if #caps == 0 then return nil end
    local order, n = {}, 0
    for pos in fmt:gmatch("%%(%d*)%$?[sd]") do
        n = n + 1
        order[n] = tonumber(pos) or n
    end
    local args = {}
    for i, c in ipairs(caps) do args[order[i] or i] = c end
    return args
end

function ns.MatchItemLine(line, pattern)
    if not pattern or type(line) ~= "string" then return nil end
    local caps = { line:match(pattern) }
    if #caps == 0 then return nil end
    local link, count
    for _, c in ipairs(caps) do
        if c:find("item:", 1, true) then
            link = c
        elseif c:match("^%d+$") then
            count = tonumber(c)
        end
    end
    if not link then return nil end
    return link, count or 1
end

local QUALITY_COLOURS = { ["9d9d9d"] = 0, ffffff = 1, ["1eff00"] = 2, ["0070dd"] = 3, a335ee = 4, ff8000 = 5 }
function ns.QualityFromLink(link)
    if type(link) ~= "string" then return nil end
    local colour = link:match("^|c%x%x(%x%x%x%x%x%x)")
    return colour and QUALITY_COLOURS[colour:lower()]
end

function ns.ItemNameFromLink(link)
    if type(link) ~= "string" then return nil end
    return link:match("|h%[(.-)%]|h")
end

function ns.AgeHours(years, months, days, hours)
    return (((tonumber(years) or 0) * 12 + (tonumber(months) or 0)) * 30 + (tonumber(days) or 0)) * 24
        + (tonumber(hours) or 0)
end

function ns.Copy(v)
    if type(v) ~= "table" then return v end
    local out = {}
    for k, x in pairs(v) do out[k] = ns.Copy(x) end
    return out
end

function ns.ItemNameIcon(itemId, link, slot)
    local r = { ns.Call("GetItemInfo", link or itemId) }
    local name, icon = r[1], tonumber(r[10])
    if type(name) ~= "string" or ns.IsSecret(name) or name == "" then name = nil else name = ns.Clean(name, 80) end
    if not icon and slot and ns.Func("GetInventoryItemTexture") then icon = tonumber((ns.Call("GetInventoryItemTexture", "player", slot))) end
    if not icon and itemId and ns.Func("C_Item.GetItemIconByID") then icon = tonumber((ns.Call("C_Item.GetItemIconByID", itemId))) end
    if icon and (ns.IsSecret(icon) or icon <= 0) then icon = nil end
    return name, icon
end

function ns.ItemSet(itemId, link, slot)
    local setId = tonumber((select(16, ns.Call("GetItemInfo", link or itemId))))
    if not setId or setId <= 0 then return nil end
    local tip
    if slot and ns.Func("C_TooltipInfo.GetInventoryItem") then
        tip = ns.Call("C_TooltipInfo.GetInventoryItem", "player", slot)
    elseif link and ns.Func("C_TooltipInfo.GetHyperlink") then
        tip = ns.Call("C_TooltipInfo.GetHyperlink", link)
    elseif itemId and ns.Func("C_TooltipInfo.GetItemByID") then
        tip = ns.Call("C_TooltipInfo.GetItemByID", itemId)
    end
    local name, pieces
    for _, line in ipairs(type(tip) == "table" and type(tip.lines) == "table" and tip.lines or {}) do
        local text = type(line) == "table" and line.leftText
        if type(text) == "string" and not ns.IsSecret(text) then
            local n, _, total = text:match("^(.-) %((%d+)/(%d+)%)$")
            if n and n ~= "" then
                name, pieces = n, tonumber(total)
                break
            end
        end
    end
    if not name then
        name = ns.Func("C_Item.GetItemSetInfo") and ns.Call("C_Item.GetItemSetInfo", setId)
            or ns.Func("GetItemSetInfo") and ns.Call("GetItemSetInfo", setId) or nil
        if type(name) ~= "string" then name = nil end
    end
    return setId, name and ns.Clean(name, 80) or nil, pieces and pieces > 0 and pieces <= 50 and pieces or nil
end

return ns
