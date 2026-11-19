local _, ns = ...

local EVERY = 15 * 60

local pending = false
local function later(name, ...)
    if pending then return end
    if not (C_Timer and C_Timer.After) then return end
    local args = { ... }
    pending = true
    C_Timer.After(5, function()
        pending = false
        if ns[name] then pcall(ns[name], unpack(args)) end
    end)
end

local function readAll(why)
    for _, name in ipairs({ "ReadStats", "ReadCurrencies", "ReadInventory", "ReadSpellbook", "ReadCollections" }) do
        if ns[name] then
            local ok, err = pcall(ns[name])
            if not ok then ns.Log("error", "Character " .. why .. ": " .. tostring(err)) end
        end
    end
end
ns.ReadCharacter = readAll

ns.On("PLAYER_LOGIN", function()
    if C_Timer and C_Timer.After then C_Timer.After(20, function() readAll("login") end) end
    if C_Timer and C_Timer.NewTicker then C_Timer.NewTicker(EVERY, function() readAll("checkpoint") end) end
end, "Character.login")
ns.On("PLAYER_LOGOUT", function() readAll("logout") end, "Character.logout")
ns.On("PLAYER_LEVEL_UP", function() later("ReadStats") end, "Character.levelUp")
ns.On("CURRENCY_DISPLAY_UPDATE", function() later("ReadCurrencies") end, "Character.currency")
ns.On("LEARNED_SPELL_IN_SKILL_LINE", function() later("ReadSpellbook") end, "Character.spells")
ns.On("NEW_MOUNT_ADDED", function() later("ReadCollections") end, "Character.mount")
ns.On("ACHIEVEMENT_EARNED", function() later("ReadCollections") end, "Character.achievement")
ns.On("KNOWN_TITLES_UPDATE", function() later("ReadCollections") end, "Character.titles")
ns.On("BANKFRAME_OPENED", function() later("ReadInventory", true) end, "Character.bank")
ns.On("PLAYER_INTERACTION_MANAGER_FRAME_SHOW", function(kind)
    local e = type(Enum) == "table" and Enum.PlayerInteractionType
    if type(e) == "table" and kind ~= nil and kind == e.Banker then later("ReadInventory", true) end
end, "Character.bankFrame")
