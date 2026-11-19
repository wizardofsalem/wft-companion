local _, ns = ...

local slots = {}
local fishing = false
local expected = {}

local function snapshot()
    slots = {}
    expected = {}
    fishing = ns.Call("IsFishingLoot") and true or false
    for slot = 1, ns.Call("GetNumLootItems") or 0 do
        local link = ns.Call("GetLootSlotLink", slot)
        local itemId = ns.ItemIdFromLink(link)
        if itemId then
            local _, name, quantity, _, quality = ns.Call("GetLootSlotInfo", slot)
            slots[slot] = {
                itemId = itemId,
                name = ns.Clean(name, 80),
                count = quantity or 1,
                quality = quality,
                sourceGUID = ns.Call("GetLootSourceInfo", slot),
            }
            local e = slots[slot]
            e.setId, e.setName, e.setPieces = ns.ItemSet(itemId, link)
            e.context = ns.ItemContext(link)
            if ns.NoteItem then ns.NoteItem(itemId, link) end
        end
    end
end

function ns.LootWindowHas(itemId)
    if (expected[itemId] or 0) > 0 and ns.Active("loot", 5) then
        expected[itemId] = expected[itemId] - 1
        return true
    end
    if ns.Active("loot", 0) then
        for _, s in pairs(slots) do
            if s.itemId == itemId and not s.chatSeen then
                s.chatSeen = true
                return true
            end
        end
    end
    return false
end

local function sourceOf(s)
    if fishing then return { kind = "fishing" } end
    local kind, id = ns.ParseGUID(s.sourceGUID)
    if kind == "pet" or kind == "player" then kind = "unknown" end
    local source = { kind = kind, id = id }
    local target = kind == "creature" and ns.Call("UnitGUID", "target")
    if target and target == s.sourceGUID then
        source.name = ns.Clean((ns.Call("UnitName", "target")), 60)
    end
    return source
end

ns.On("LOOT_OPENED", function()
    if ns.Enabled("loot") then snapshot() end
end, "Loot.open")

ns.On("LOOT_SLOT_CLEARED", function(slot)
    local s = slots[slot]
    if not s or not ns.Enabled("loot") then return end
    slots[slot] = nil
    if not s.chatSeen then expected[s.itemId] = (expected[s.itemId] or 0) + 1 end
    ns.AddHistory("loot", {
        at = time(),
        itemId = s.itemId,
        name = s.name,
        quality = s.quality,
        setId = s.setId, setName = s.setName, setPieces = s.setPieces,
        context = s.context,
        count = s.count,
        how = "loot",
        source = sourceOf(s),
        zone = ns.Clean(ns.Call("GetRealZoneText"), 60),
        subzone = ns.Clean(ns.Call("GetSubZoneText"), 60),
    }, tostring(s.itemId) .. ":" .. tostring(s.sourceGUID) .. ":" .. slot)
end, "Loot.slot")

ns.On("LOOT_CLOSED", function()
    slots = {}
end, "Loot.close")
