local _, ns = ...

local function api()
    if ns.Func("GetNumFactions") and ns.Func("GetFactionInfo") then
        return {
            num = function() return ns.Call("GetNumFactions") end,
            at = function(i)
                local name, _, standingId, barMin, barMax, barValue, _, _, isHeader, isCollapsed, hasRep, _, _, factionId =
                    ns.Call("GetFactionInfo", i)
                return name, isHeader, isCollapsed, standingId, barMin, barMax, barValue, hasRep, factionId
            end,
            expand = function(i) ns.Call("ExpandFactionHeader", i) end,
            collapse = function(i) ns.Call("CollapseFactionHeader", i) end,
        }
    end
    if ns.Func("C_Reputation.GetNumFactions") and ns.Func("C_Reputation.GetFactionDataByIndex") then
        return {
            num = function() return ns.Call("C_Reputation.GetNumFactions") end,
            at = function(i)
                local d = ns.Call("C_Reputation.GetFactionDataByIndex", i)
                if type(d) ~= "table" then return nil end
                return d.name, d.isHeader, d.isCollapsed, d.reaction, d.currentReactionThreshold,
                    d.nextReactionThreshold, d.currentStanding, d.isHeaderWithRep, d.factionID
            end,
            expand = function(i) ns.Call("C_Reputation.ExpandFactionHeader", i) end,
            collapse = function(i) ns.Call("C_Reputation.CollapseFactionHeader", i) end,
        }
    end
    ns.NoteMissing(ns.Func("GetNumFactions") and "GetFactionInfo" or "GetNumFactions")
end

local function capture()
    if ns.InCombat() then return ns.AfterCombat("reputations", capture) end
    if not ns.Enabled("reputations") then return end
    local c = ns.Character()
    local r = api()
    if not c or not r or not r.num() then return end

    local opened = {}
    local i = 1
    while i <= (r.num() or 0) and i < 500 do
        local name, isHeader, isCollapsed = r.at(i)
        if isHeader and isCollapsed and name then
            r.expand(i)
            opened[name] = true
        end
        i = i + 1
    end

    local factions = {}
    for j = 1, math.min(r.num() or 0, 500) do
        local name, isHeader, _, standingId, barMin, barMax, barValue, hasRep, factionId = r.at(j)
        if name and (not isHeader or hasRep) and standingId then
            factions[#factions + 1] = {
                id = factionId,
                name = ns.Clean(name, 60),
                standingId = standingId,
                barValue = barValue,
                barMin = barMin,
                barMax = barMax,
            }
        end
    end

    for j = (r.num() or 0), 1, -1 do
        local name, isHeader, isCollapsed = r.at(j)
        if isHeader and not isCollapsed and opened[name] then
            r.collapse(j)
        end
    end

    if #factions > 0 then
        ns.SetState("reputations", ns.StateKey(c), { factions = factions })
    end
end

ns.On("PLAYER_LOGOUT", capture, "Reputations.capture")
