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

function ns.NoteItem(itemId, link)
    if not itemId or not ns.Enabled("itemFacts") then return end
    ns.db.itemsSeen = ns.db.itemsSeen or {}
    if ns.db.itemsSeen[itemId] then return end
    local r = { ns.Call("GetItemInfo", link or itemId) }
    if type(r[1]) ~= "string" or ns.IsSecret(r[1]) then return end
    ns.db.itemsSeen[itemId] = true
    local fields = { itemId = itemId, name = ns.Clean(r[1], 80), quality = whole(r[3]), minLevel = whole(r[5]),
        stackCount = whole(r[8]), sellPrice = whole(r[11]), classId = whole(r[12]), subclassId = whole(r[13]),
        bindType = whole(r[14]), expansionId = whole(r[15]), setId = whole(r[16]), icon = whole(r[10]) }
    if type(r[9]) == "string" and not ns.IsSecret(r[9]) and r[9] ~= "" then fields.equipLoc = ns.Clean(r[9], 30) end
    if fields.setId and fields.setId <= 0 then fields.setId = nil end
    ns.AddHistory("itemFacts", fields, itemId .. ":" .. ns.NextSeq())
end
