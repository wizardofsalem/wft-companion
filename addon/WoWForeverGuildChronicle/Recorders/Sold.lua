local _, ns = ...

local NUM_BAGS = 4
local SETTLE = 1

local open = false
local base
local baseMoney
local sales = {}
local BUYBACK_SLOTS = 12

local function slotItem(bag, slot)
    local C = C_Container
    if C and C.GetContainerItemInfo then
        local info = C.GetContainerItemInfo(bag, slot)
        if info then return info.itemID, info.stackCount, info.hyperlink end
        return
    end
    local _, count, _, _, _, _, link, _, _, itemId = ns.Call("GetContainerItemInfo", bag, slot)
    return itemId or ns.ItemIdFromLink(link), count, link
end

local function numSlots(bag)
    local C = C_Container
    if C and C.GetContainerNumSlots then return C.GetContainerNumSlots(bag) or 0 end
    return ns.Call("GetContainerNumSlots", bag) or 0
end

function ns.Bags()
    local out = {}
    for bag = 0, NUM_BAGS do
        for slot = 1, numSlots(bag) do
            local itemId, count, link = slotItem(bag, slot)
            if itemId then
                local e = out[itemId] or { count = 0, link = link }
                e.count = e.count + (count or 1)
                out[itemId] = e
            end
        end
    end
    return out
end

local function merchant()
    local kind, id = ns.ParseGUID(ns.Call("UnitGUID", "npc"))
    if kind ~= "creature" then return { kind = "unknown" } end
    return { kind = "creature", id = id, name = ns.Clean(ns.Call("UnitName", "npc"), 60) }
end

local function itemFields(itemId, link, count)
    local name, _, quality, _, _, _, _, _, _, _, sellPrice = ns.Call("GetItemInfo", link or itemId)
    return {
        at = time(),
        itemId = itemId,
        name = ns.Clean(ns.ItemNameFromLink(link) or name, 80),
        quality = quality,
        count = count,
    }, sellPrice
end

local function recordSale(itemId, link, count, copper)
    local fields = itemFields(itemId, link, count)
    fields.copper = copper
    fields.merchant = merchant()
    fields.zone = ns.Clean(ns.Call("GetRealZoneText"), 60)
    fields.subzone = ns.Clean(ns.Call("GetSubZoneText"), 60)
    local r = ns.AddHistory("sold", fields, "sold:" .. itemId .. ":" .. ns.NextSeq())
    if r then
        ns.Push(sales, r, BUYBACK_SLOTS)
        ns.Debug("sold " .. itemId .. " x" .. count .. " for " .. tostring(copper))
    end
end

local function undo(sale)
    for i = #sales, 1, -1 do
        if sales[i] == sale then table.remove(sales, i) end
    end
    local list = ns.db.history.sold and ns.db.history.sold[ns.CharKey(sale.character)]
    for i = #(list or {}), 1, -1 do
        if list[i].id == sale.id then
            table.remove(list, i)
            ns.Debug("bought back " .. sale.id)
            return
        end
    end
end

local function findSale(itemId, count, paid)
    for i = #sales, 1, -1 do
        local s = sales[i]
        if s.itemId == itemId and s.count == count and s.copper == paid then return s end
    end
end

local function sold(gone, gain)
    if #gone == 1 then
        gone[1].copper = gain
        return gone
    end
    local sum, match = 0, {}
    for _, g in ipairs(gone) do
        local worth = g.price and g.price * g.count
        if worth == gain then match[#match + 1] = g end
        sum = (sum and worth) and sum + worth or nil
    end
    if sum == gain then
        for _, g in ipairs(gone) do g.copper = g.price * g.count end
        return gone
    end
    if #match == 1 then
        match[1].copper = gain
        return match
    end
    return gone
end

local function settle(final)
    if not base then return end
    local now = ns.Bags()
    local money = ns.Call("GetMoney")
    if not money then return end
    local delta = money - baseMoney
    local gone, came = {}, {}
    for itemId, e in pairs(base) do
        local left = e.count - (now[itemId] and now[itemId].count or 0)
        if left > 0 then gone[#gone + 1] = { itemId = itemId, link = e.link, count = left } end
    end
    for itemId, e in pairs(now) do
        local more = e.count - (base[itemId] and base[itemId].count or 0)
        if more > 0 then came[#came + 1] = { itemId = itemId, link = e.link, count = more } end
    end

    if #gone > 0 and delta > 0 then
        for _, g in ipairs(gone) do
            local _, price = itemFields(g.itemId, g.link, g.count)
            g.price = price
        end
        for _, g in ipairs(sold(gone, delta)) do recordSale(g.itemId, g.link, g.count, g.copper) end
    elseif #came == 1 and delta < 0 then
        local sale = findSale(came[1].itemId, came[1].count, -delta)
        if sale then undo(sale) end
    elseif not final and (#gone > 0 or delta > 0) then
        if C_Timer then C_Timer.After(SETTLE, function() settle(true) end) end
        return
    end
    base, baseMoney = now, money
end

ns.On("MERCHANT_SHOW", function()
    if not ns.Enabled("sold") then return end
    open = true
    base, baseMoney = ns.Bags(), ns.Call("GetMoney")
    if not baseMoney then base = nil end
end, "Sold.show")

local function onChange()
    if base then settle(false) end
end
ns.On("BAG_UPDATE_DELAYED", onChange, "Sold.bags")
ns.On("PLAYER_MONEY", onChange, "Sold.money")

ns.On("MERCHANT_CLOSED", function()
    if not open then return end
    open = false
    local function finish()
        if open then return end
        settle(true)
        base, baseMoney = nil, nil
    end
    if C_Timer then C_Timer.After(SETTLE, finish) else finish() end
end, "Sold.close")
