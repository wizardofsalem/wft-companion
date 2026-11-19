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

local BAGS = { 0, 1, 2, 3, 4, 5 }
local BANK = { -1, 6, 7, 8, 9, 10, 11, 12 }
local bankItems

local function readBags(ids)
    local totals, order, free, slots = {}, {}, 0, 0
    for _, bag in ipairs(ids) do
        local n = tonumber((ns.Call("C_Container.GetContainerNumSlots", bag))) or 0
        if n > 0 and not ns.IsSecret(n) then
            slots = slots + n
            free = free + (tonumber((ns.Call("C_Container.GetContainerNumFreeSlots", bag))) or 0)
            for s = 1, n do
                local info = ns.Call("C_Container.GetContainerItemInfo", bag, s)
                if type(info) == "table" and not ns.IsSecret(info) then
                    local id, count = whole(info.itemID), whole(info.stackCount) or 1
                    if id then
                        if not totals[id] then totals[id] = 0; order[#order + 1] = id end
                        totals[id] = totals[id] + count
                        if ns.NoteItem then ns.NoteItem(id, info.hyperlink) end
                    end
                end
            end
        end
    end
    local list = {}
    table.sort(order)
    for _, id in ipairs(order) do
        if #list < 400 then list[#list + 1] = { itemId = id, count = totals[id] } end
    end
    return list, free, slots
end

local function inventory(bankOpen)
    if not ns.Enabled("inventory") or not ns.Func("C_Container.GetContainerNumSlots") then return end
    local c = ns.Character()
    if not c then return end
    local bags, free, total = readBags(BAGS)
    if total == 0 then return end
    if bankOpen then bankItems = (readBags(BANK)) end
    local f = { bags = bags, free = free, total = total }
    if bankItems and #bankItems > 0 then f.bank = bankItems end
    ns.SetState("inventory", ns.StateKey(c), f)
end

ns.ReadInventory = inventory
