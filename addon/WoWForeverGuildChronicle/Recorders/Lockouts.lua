local _, ns = ...

local function read()
    if not ns.Enabled("lockouts") then return end
    local c = ns.Character()
    if not c then return end
    local n = tonumber((ns.Call("GetNumSavedInstances"))) or 0
    local list = {}
    for i = 1, math.min(n, 50) do
        local name, lockoutId, reset, difficultyId, locked, extended, _, isRaid, maxPlayers, difficultyName,
            numEncounters, encounterProgress = ns.Call("GetSavedInstanceInfo", i)
        if name and not ns.IsSecret(name) then
            list[#list + 1] = {
                name = ns.Clean(name, 80),
                lockoutId = tonumber(lockoutId),
                difficultyId = tonumber(difficultyId),
                difficulty = type(difficultyName) == "string" and ns.Clean(difficultyName, 40) or nil,
                locked = locked and true or false,
                extended = extended and true or nil,
                raid = isRaid and true or false,
                maxPlayers = tonumber(maxPlayers),
                encounters = tonumber(numEncounters),
                progress = tonumber(encounterProgress),
                resetAt = tonumber(reset) and (math.floor((time() + tonumber(reset)) / 60) * 60) or nil,
            }
        end
    end
    ns.SetState("lockouts", ns.StateKey(c), { instances = list })
end

ns.On("UPDATE_INSTANCE_INFO", function() pcall(read) end, "Lockouts.update")
ns.On("PLAYER_LOGIN", function()
    if C_Timer and C_Timer.After then
        C_Timer.After(15, function()
            ns.AfterCombat("lockouts:request", function()
                if ns.Func("RequestRaidInfo") then ns.Call("RequestRaidInfo") end
            end)
        end)
    end
end, "Lockouts.login")
