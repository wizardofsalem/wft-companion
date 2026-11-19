local _, ns = ...

local INTERVAL = 15 * 60
local lastKey

local function capture(why, level)
    if not ns.Enabled("xp") then return end
    local xp, max = tonumber((ns.Call("UnitXP", "player"))), tonumber((ns.Call("UnitXPMax", "player")))
    level = tonumber(level) or tonumber((ns.Call("UnitLevel", "player")))
    if not xp or not max or not level or max <= 0 then return end
    local key = level .. ":" .. xp
    if key == lastKey and why ~= "logout" then return end
    lastKey = key
    ns.AddHistory("xp", {
        at = time(),
        level = level,
        xp = xp,
        xpMax = max,
        rested = tonumber((ns.Call("GetXPExhaustion"))) or 0,
        why = why,
    }, why .. ":" .. key .. ":" .. ns.NextSeq())
end

ns.On("PLAYER_LOGIN", function()
    if C_Timer then
        C_Timer.After(5, function() capture("login") end)
        if C_Timer.NewTicker then C_Timer.NewTicker(INTERVAL, function() capture("tick") end) end
    end
end, "XP.login")
ns.On("PLAYER_LEVEL_UP", function(level)
    if C_Timer then C_Timer.After(1, function() capture("level", level) end) else capture("level", level) end
end, "XP.level")
ns.On("PLAYER_LOGOUT", function() capture("logout") end, "XP.logout")
