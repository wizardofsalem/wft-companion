local _, ns = ...

local function safe(fn)
    return function(...)
        local ok, err = pcall(fn, ...)
        if not ok then ns.Log("error", "Social: " .. tostring(err)) end
    end
end

local lastEmote
local pendingRead
local function record(tok, tgt)
    local key = tostring(tok) .. "|" .. tostring(tgt) .. "|" .. time()
    if key == lastEmote then return end
    lastEmote = key
    ns.DayCount("emotes")
    ns.AddHistory("social", { at = time(), kind = "emote", emote = tok, target = tgt }, "emote:" .. ns.NextSeq())
end
local function emote(token, target)
    local who
    if type(target) == "string" and target ~= "" and not ns.IsSecret(target) then
        who = target
    elseif ns.Call("UnitIsPlayer", "target") then
        who = ns.Call("UnitName", "target")
    end
    local tok = type(token) == "string" and not ns.IsSecret(token) and ns.Clean(token, 30) or nil
    local tgt = type(who) == "string" and not ns.IsSecret(who) and ns.Clean(who, 60) or nil
    if tok == "READ" then
        pendingRead = { tok = tok, tgt = tgt, at = GetTime() }
        return
    end
    record(tok, tgt)
end
ns.HookGame("DoEmote", safe(emote))
ns.HookGame("C_ChatInfo.PerformEmote", safe(emote))
ns.On("CHAT_MSG_TEXT_EMOTE", safe(function(_, sender, _, _, _, _, _, _, _, _, _, guid)
    local p = pendingRead
    pendingRead = nil
    if not p or GetTime() - p.at > 1.5 then return end
    local c = ns.Character()
    if not c then return end
    local own
    if guid and ns.IsSecret(guid) == false and c.guid ~= "" then
        own = guid == c.guid
    elseif type(sender) == "string" and not ns.IsSecret(sender) then
        local full = c.surname and (c.name .. " " .. c.surname)
        own = sender == c.name or (full and ns.SamePlayer(sender, full))
    end
    if own then record(p.tok, p.tgt) end
end), "Social.textEmote")

ns.On("PARTY_INVITE_REQUEST", safe(function() ns.DayCount("partyInvitesReceived") end), "Social.partyInvite")
ns.On("GUILD_INVITE_REQUEST", safe(function() ns.DayCount("guildInvitesReceived") end), "Social.guildInvite")
if not ns.HookGame("C_PartyInfo.InviteUnit", safe(function() ns.DayCount("partyInvitesSent") end)) then
    ns.HookGame("InviteUnit", safe(function() ns.DayCount("partyInvitesSent") end))
end
if not ns.HookGame("C_GuildInfo.Invite", safe(function() ns.DayCount("guildInvitesSent") end)) then
    ns.HookGame("GuildInvite", safe(function() ns.DayCount("guildInvitesSent") end))
end

local function lines(sentField, heardField)
    return safe(function(_, sender, _, _, _, _, _, _, _, _, _, guid)
        ns.DayCount(heardField)
        local c = ns.Character()
        if not c then return end
        if guid and ns.IsSecret(guid) == false and c.guid ~= "" and guid == c.guid then
            ns.DayCount(sentField)
        elseif not guid and type(sender) == "string" and not ns.IsSecret(sender) then
            local full = c.surname and (c.name .. " " .. c.surname)
            if sender == c.name or (full and ns.SamePlayer(sender, full)) then ns.DayCount(sentField) end
        end
    end)
end
ns.On("CHAT_MSG_SAY", lines("saySent", "sayHeard"), "Social.say")
ns.On("CHAT_MSG_YELL", lines("yellSent", "yellHeard"), "Social.yell")
