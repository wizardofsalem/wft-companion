local _, ns = ...

local FORMATS = { { "DUEL_WINNER_KNOCKOUT", "knockout" }, { "DUEL_WINNER_RETREAT", "retreat" } }

local function describe(name)
    for _, u in ipairs({ "player", "target", "mouseover" }) do
        local n = ns.Call("UnitName", u)
        if n and n == name then return ns.UnitPlayer(u) or { name = ns.Clean(name, 60) } end
    end
    return { name = ns.Clean(name, 60) }
end

ns.On("CHAT_MSG_SYSTEM", function(line)
    if not ns.Enabled("duel") or type(line) ~= "string" then return end
    for _, f in ipairs(FORMATS) do
        local args = ns.FormatArgs(_G[f[1]], line)
        if args and args[1] and args[2] then
            ns.AddHistory("duel", {
                at = time(),
                winner = describe(args[1]),
                loser = describe(args[2]),
                how = f[2],
                zone = ns.Clean(ns.Call("GetRealZoneText"), 60),
                subzone = ns.Clean(ns.Call("GetSubZoneText"), 60),
            }, args[1] .. ":" .. args[2] .. ":" .. ns.NextSeq())
            ns.Debug("duel: " .. args[1] .. " beat " .. args[2])
            return
        end
    end
end, "Duel.system")
