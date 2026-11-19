local _, ns = ...

local KEEP_DAYS = 30

local lastDay

local function zeros()
    local t = {}
    for i = 1, 24 do t[i] = 0 end
    return t
end

local function today() return date("%Y-%m-%d", time()) end

local function byZone(old)
    local list = {}
    if type(old) == "table" then
        for _, e in ipairs(old) do
            if type(e) == "table" and type(e.zone) == "string" then
                list[#list + 1] = { zone = e.zone, sent = tonumber(e.sent) or 0 }
            end
        end
    end
    local zone = ns.Clean(ns.Call("GetRealZoneText"), 60)
    if not zone or zone == "" then return list end
    for _, e in ipairs(list) do
        if e.zone == zone then
            e.sent = e.sent + 1
            return list
        end
    end
    list[#list + 1] = { zone = zone, sent = 1 }
    return list
end

local function prune()
    local bucket = ns.db.state.guildChat
    if not bucket then return end
    local cutoff = date("%Y-%m-%d", time() - KEEP_DAYS * 86400)
    for key, r in pairs(bucket) do
        if type(r) ~= "table" or type(r.day) ~= "string" or r.day < cutoff then bucket[key] = nil end
    end
end

ns.On("CHAT_MSG_GUILD", function(_, sender, _, _, _, _, _, _, _, _, _, guid)
    if not ns.Enabled("guildChat") then return end
    local c = ns.Character()
    if not c then return end
    local day = today()
    if day ~= lastDay then
        prune()
        lastDay = day
    end
    local key = ns.StateKey(c) .. "|" .. day
    local old = (ns.db.state.guildChat or {})[key]
    local hour = tonumber(date("%H", time())) + 1
    local sentH, seenH = zeros(), zeros()
    if old then
        for i = 1, 24 do
            sentH[i] = type(old.sentByHour) == "table" and tonumber(old.sentByHour[i]) or 0
            seenH[i] = type(old.seenByHour) == "table" and tonumber(old.seenByHour[i]) or 0
        end
    end
    seenH[hour] = seenH[hour] + 1
    local fields = {
        sentByHour = sentH,
        seenByHour = seenH,
        day = day,
        guild = ns.Clean((ns.Call("GetGuildInfo", "player")), 60),
        sent = old and old.sent or 0,
        seen = (old and old.seen or 0) + 1,
        unreadable = old and old.unreadable or nil,
        sentByZone = old and old.sentByZone or nil,
    }
    if guid then
        if c.guid ~= "" and guid == c.guid then
            fields.sent = fields.sent + 1
            sentH[hour] = sentH[hour] + 1
            fields.sentByZone = byZone(fields.sentByZone)
        end
    elseif sender then
        local full = c.surname and (c.name .. " " .. c.surname)
        if sender == c.name or sender == c.name .. "-" .. c.realm:gsub("%s", "")
            or (full and ns.SamePlayer(sender, full)) then
            fields.sent = fields.sent + 1
            sentH[hour] = sentH[hour] + 1
            fields.sentByZone = byZone(fields.sentByZone)
        end
    else
        fields.unreadable = (fields.unreadable or 0) + 1
    end
    ns.SetState("guildChat", key, fields)
end, "GuildChat.message")
