local _, ns = ...

local guildName
local signatures
local signers

local function readable(v)
    return v ~= nil and not ns.IsSecret(v)
end

local function readPetition(fresh)
    if fresh then guildName, signatures, signers = nil, nil, nil end
    if ns.Func("GetPetitionInfo") then
        local r = { ns.Call("GetPetitionInfo") }
        local title = r[2]
        if type(title) == "string" and readable(title) and title ~= "" then guildName = ns.Clean(title, 60) end
    end
    if ns.Func("GetNumPetitionNames") then
        local n = ns.Call("GetNumPetitionNames")
        if type(n) == "number" and readable(n) and n >= 0 then
            signatures = n
            if ns.Func("GetPetitionNameInfo") then
                local names = {}
                for i = 1, math.min(n, 9) do
                    local who = ns.Call("GetPetitionNameInfo", i)
                    if type(who) == "string" and readable(who) and who ~= "" then names[#names + 1] = ns.Clean(who, 60) end
                end
                signers = #names > 0 and names or nil
            end
        end
    end
end

local function record(action)
    if not ns.Enabled("charter") then return end
    if not guildName then
        ns.Debug("charter " .. action .. ": no guild name on the petition, nothing recorded")
        return
    end
    local fields = { guild = guildName, action = action }
    if signatures then fields.signatures = signatures end
    if signers then fields.signers = signers end
    ns.AddHistory("charter", fields, action .. ":" .. guildName .. ":" .. ns.NextSeq())
    ns.Debug("charter " .. action .. ": " .. guildName)
end

ns.On("PETITION_SHOW", function() readPetition(true) end, "Charter.show")
ns.On("PETITION_CLOSED", function() guildName, signatures, signers = nil, nil, nil end, "Charter.closed")

local function hook(name, action)
    if type(_G[name]) ~= "function" or type(hooksecurefunc) ~= "function" then
        ns.NoteMissing(name)
        return
    end
    if not pcall(hooksecurefunc, name, function()
        pcall(function()
            readPetition(false)
            record(action)
        end)
    end) then
        ns.NoteMissing(name)
    end
end

ns.On("PLAYER_LOGIN", function()
    hook("SignPetition", "signed")
    hook("TurnInGuildCharter", "founded")
end, "Charter.login")
