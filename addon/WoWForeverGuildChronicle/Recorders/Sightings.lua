local _, ns = ...

local KEEP_DAYS = 30
local MAX_KNOWN = 5000

local function today() return math.floor(time() / 86400) end

local function note(p, via)
    if not p or not ns.Enabled("sighting") then return end
    if p.guid and p.guid == ns.Call("UnitGUID", "player") then return end
    local key = p.guid or p.name
    local sig = tostring(p.level) .. "|" .. tostring(p.guild) .. "|" .. tostring(p.guildRank)
    local known = ns.db.seen[key]
    if known and known.sig == sig and known.day == today() then return end
    ns.db.seen[key] = { sig = sig, day = today() }
    local fields = ns.Copy(p)
    fields.at = time()
    fields.via = via
    ns.AddHistory("sighting", fields, key .. ":" .. ns.NextSeq())
end

local function unit(u, via)
    if not ns.Enabled("sighting") then return end
    note(ns.UnitPlayer(u), via)
end

ns.On("PLAYER_TARGET_CHANGED", function() unit("target", "target") end, "Sightings.target")
ns.On("UPDATE_MOUSEOVER_UNIT", function() unit("mouseover", "mouseover") end, "Sightings.mouseover")
ns.On("NAME_PLATE_UNIT_ADDED", function(u) if u then unit(u, "nameplate") end end, "Sightings.nameplate")

local function whoResult(i)
    local list = C_FriendList
    if type(list) == "table" and type(list.GetWhoInfo) == "function" then
        local ok, w = pcall(list.GetWhoInfo, i)
        if ok and type(w) == "table" then
            return w.fullName, w.fullGuildName, w.level, w.filename
        end
    end
    local name, guild, level, _, _, _, classFile = ns.Call("GetWhoInfo", i)
    return name, guild, level, classFile
end

local function whoCount()
    local list = C_FriendList
    if type(list) == "table" and type(list.GetNumWhoResults) == "function" then
        local ok, n = pcall(list.GetNumWhoResults)
        if ok then return tonumber(n) or 0 end
    end
    return tonumber((ns.Call("GetNumWhoResults"))) or 0
end

ns.On("WHO_LIST_UPDATE", function()
    if not ns.Enabled("sighting") then return end
    for i = 1, math.min(whoCount(), 50) do
        local name, guild, level, classFile = whoResult(i)
        if name and not ns.IsSecret(name) and name ~= "" then
            note({
                name = ns.Clean(name, 60),
                guild = guild and guild ~= "" and ns.Clean(guild, 60) or nil,
                level = tonumber(level),
                class = classFile,
            }, "who")
        end
    end
end, "Sightings.who")

ns.On("PLAYER_LOGIN", function()
    local cutoff, n = today() - KEEP_DAYS, 0
    for key, k in pairs(ns.db.seen) do
        if type(k) ~= "table" or (k.day or 0) < cutoff then ns.db.seen[key] = nil else n = n + 1 end
    end
    if n > MAX_KNOWN then ns.db.seen = {} end
end, "Sightings.prune")
