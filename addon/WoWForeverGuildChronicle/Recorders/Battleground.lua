local _, ns = ...

local MAX_PLAYERS = 80
local done = false
local requested = false

local FACTIONS = { [0] = "horde", [1] = "alliance" }

local function pvpRank(raw)
    raw = tonumber(raw)
    if not raw or raw < 5 then return 0 end
    return raw - 4
end

local function winner()
    local w = ns.Call("GetBattlefieldWinner")
    if w == nil then return nil end
    return FACTIONS[w] or "draw"
end

local function objectiveStats(i)
    if not ns.Func("C_PvP.GetScoreInfo") then return nil end
    local info = ns.Call("C_PvP.GetScoreInfo", i)
    if type(info) ~= "table" or ns.IsSecret(info) or type(info.stats) ~= "table" then return nil end
    local out = {}
    for _, st in ipairs(info.stats) do
        local id, value = type(st) == "table" and tonumber(st.pvpStatID), type(st) == "table" and tonumber(st.pvpStatValue)
        if id and value and not ns.IsSecret(id) and not ns.IsSecret(value) and #out < 20 then
            out[#out + 1] = { id = math.floor(id), value = math.floor(value) }
        end
    end
    return #out > 0 and out or nil
end

local function capture()
    if done or not ns.Enabled("bgMatch") then return end
    local result = winner()
    if not result then return end
    local count = tonumber((ns.Call("GetNumBattlefieldScores"))) or 0
    if count == 0 then return end
    local players = {}
    for i = 1, math.min(count, MAX_PLAYERS) do
        local name, killingBlows, honorableKills, deaths, honorGained, faction, rank, _, _, classToken =
            ns.Call("GetBattlefieldScore", i)
        if name and not ns.IsSecret(name) then
            players[#players + 1] = {
                name = ns.Clean(name, 60),
                faction = FACTIONS[faction],
                class = classToken,
                rank = pvpRank(rank),
                killingBlows = tonumber(killingBlows),
                honorableKills = tonumber(honorableKills),
                deaths = tonumber(deaths),
                honor = tonumber(honorGained),
                stats = objectiveStats(i),
            }
        end
    end
    if #players == 0 then return end
    done = true
    table.sort(players, function(a, b) return a.name < b.name end)
    local bg, _, _, _, _, _, _, instanceId = ns.Call("GetInstanceInfo")
    local runMs = tonumber((ns.Call("GetBattlefieldInstanceRunTime")))
    ns.AddHistory("bgMatch", {
        at = time(),
        battleground = ns.Clean(bg, 60),
        instanceId = tonumber(instanceId),
        winner = result,
        durationSeconds = runMs and runMs > 0 and math.floor(runMs / 1000) or nil,
        players = players,
    }, tostring(bg) .. ":" .. ns.NextSeq())
    ns.Debug("battleground " .. tostring(bg) .. ": " .. result .. " won, " .. #players .. " players")
end

ns.On("UPDATE_BATTLEFIELD_STATUS", function()
    if done or requested or not ns.Enabled("bgMatch") or not winner() then return end
    requested = true
    ns.Call("RequestBattlefieldScoreData")
end, "Battleground.status")
ns.On("UPDATE_BATTLEFIELD_SCORE", capture, "Battleground.score")
ns.On("PLAYER_ENTERING_WORLD", function()
    done, requested = false, false
end, "Battleground.reset")
