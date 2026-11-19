local _, ns = ...

local SLOTS = 19
local LOGIN_DELAY = 5
local DEBOUNCE = 5
local pending = false

local function capture(why)
    pending = false
    if not ns.Enabled("gear") then return end
    local c = ns.Character()
    if not c then return end
    local slots = {}
    for slot = 1, SLOTS do
        local itemId = tonumber((ns.Call("GetInventoryItemID", "player", slot)))
        if itemId then
            local e = { slot = slot, itemId = itemId }
            local link = ns.Call("GetInventoryItemLink", "player", slot)
            e.name, e.icon = ns.ItemNameIcon(itemId, link, slot)
            e.context = ns.ItemContext(link)
            local quality = tonumber((ns.Call("GetInventoryItemQuality", "player", slot)))
            if quality then e.quality = quality end
            e.setId, e.setName, e.setPieces = ns.ItemSet(itemId, link, slot)
            slots[#slots + 1] = e
        end
    end
    ns.db.gearSeen = ns.db.gearSeen or {}
    local key = ns.StateKey(c)
    local before = ns.db.gearSeen[key]
    local now = {}
    for _, e in ipairs(slots) do now[e.slot] = e end
    if before and ns.Enabled("gearChange") then
        for slot = 1, SLOTS do
            local was, is = before[slot], now[slot]
            if (was or 0) ~= (is and is.itemId or 0) then
                ns.AddHistory("gearChange", { slot = slot, oldItemId = was, newItemId = is and is.itemId or nil,
                    quality = is and is.quality or nil, setId = is and is.setId or nil }, slot .. ":" .. ns.NextSeq())
            end
        end
    end
    local seen = {}
    for slot, e in pairs(now) do seen[slot] = e.itemId end
    ns.db.gearSeen[key] = seen
    for _, e in ipairs(slots) do if ns.NoteItem then ns.NoteItem(e.itemId) end end
    if ns.SetState("gear", key, { slots = slots }) then
        ns.Debug("gear (" .. why .. "): " .. #slots .. " slots")
    end
end

local function later(why, seconds)
    if pending then return end
    if not (C_Timer and C_Timer.After) then return capture(why) end
    pending = true
    C_Timer.After(seconds, function() capture(why) end)
end

ns.On("PLAYER_LOGIN", function() later("login", LOGIN_DELAY) end, "Gear.PLAYER_LOGIN")
ns.On("PLAYER_EQUIPMENT_CHANGED", function() later("change", DEBOUNCE) end, "Gear.PLAYER_EQUIPMENT_CHANGED")
