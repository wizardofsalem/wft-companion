local _, ns = ...

ns.On("PLAYER_LEVEL_UP", function(level)
    if not ns.Enabled("level") then return end
    level = tonumber(level)
    if not level then return end
    local r = ns.AddHistory("level", {
        at = time(),
        level = level,
        zone = ns.Clean(ns.Call("GetRealZoneText"), 60),
        subzone = ns.Clean(ns.Call("GetSubZoneText"), 60),
    }, "level:" .. level)
    if r then ns.TimeFinishedLevel(level, function(seconds) r.prevLevelSeconds = seconds end) end
    ns.Debug("level " .. level)
end, "Level.up")
