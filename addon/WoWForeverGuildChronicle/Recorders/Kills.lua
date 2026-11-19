local _, ns = ...

local KEEP = 30 * 86400
local TOP = 20
local MAX_NAMES = 300
local WRITE_EVERY = 60

local LINES = {
    { "COMBATLOG_XPGAIN_EXHAUSTION1_GROUP", rested = 3, group = true },
    { "COMBATLOG_XPGAIN_EXHAUSTION1_RAID", rested = 3, group = true },
    { "COMBATLOG_XPGAIN_EXHAUSTION2_GROUP", group = true },
    { "COMBATLOG_XPGAIN_EXHAUSTION2_RAID", group = true },
    { "COMBATLOG_XPGAIN_EXHAUSTION1", rested = 3 },
    { "COMBATLOG_XPGAIN_EXHAUSTION2" },
    { "COMBATLOG_XPGAIN_FIRSTPERSON_GROUP", group = true },
    { "COMBATLOG_XPGAIN_FIRSTPERSON_RAID", group = true },
    { "COMBATLOG_XPGAIN_FIRSTPERSON" },
    { "COMBATLOG_XPGAIN_FIRSTPERSON_UNNAMED", quest = true },
}

local function compile(fmt)
    if type(fmt) ~= "string" or fmt == "" then return nil end
    local out, args, n, i = { "^" }, {}, 0, 1
    while i <= #fmt do
        local c = fmt:sub(i, i)
        if c == "%" then
            local pos, kind, len = fmt:match("^(%d)%$([sd])()", i + 1)
            if pos then
                len = len - i
            else
                kind = fmt:sub(i + 1, i + 1)
                len = 2
            end
            if kind == "s" or kind == "d" then
                n = n + 1
                args[#args + 1] = tonumber(pos) or n
                out[#out + 1] = kind == "s" and "(.-)" or "([%-%+]?%d+)"
                i = i + len
            elseif kind == "%" then
                out[#out + 1] = "%%"
                i = i + 2
            else
                return nil
            end
        else
            out[#out + 1] = c:find("[%^%$%(%)%%%.%[%]%*%+%-%?]") and ("%" .. c) or c
            i = i + 1
        end
    end
    if n == 0 or n > 8 then return nil end
    out[#out + 1] = "$"
    return { pattern = table.concat(out), args = args }
end

local compiled
local function patterns()
    if compiled then return compiled end
    compiled = {}
    for _, l in ipairs(LINES) do
        local p = compile(_G[l[1]])
        if p then
            p.rested, p.group, p.quest = l.rested, l.group, l.quest
            compiled[#compiled + 1] = p
        end
    end
    return compiled
end

function ns.ParseXPLine(text)
    if type(text) ~= "string" then return nil end
    for _, p in ipairs(patterns()) do
        local caps = { text:match(p.pattern) }
        if #caps > 0 then
            local v = {}
            for i, pos in ipairs(p.args) do v[pos] = caps[i] end
            if p.quest then
                local xp = tonumber(v[1])
                if xp then return { kind = "quest", xp = xp } end
            else
                local xp = tonumber(v[2])
                if xp and v[1] and v[1] ~= "" then
                    local rested = p.rested and tonumber((tostring(v[p.rested] or ""):match("%d+"))) or nil
                    if rested and rested > xp then rested = nil end
                    return { kind = "kill", name = v[1], xp = xp, rested = rested, group = p.group or false }
                end
            end
        end
    end
end

local lastWrite = 0

local function open(c)
    local loginAt = ns.SessionId and ns.SessionId()
    if not loginAt then return nil end
    ns.db.openKills = ns.db.openKills or {}
    local key = ns.StateKey(c)
    local k = ns.db.openKills[key]
    if not k or k.loginAt ~= loginAt then
        k = { loginAt = loginAt, kills = 0, xp = 0, restedXp = 0, groupKills = 0, instanceKills = 0,
            instanceXp = 0, secretLines = 0, questXp = 0, creatures = {}, names = 0, otherKills = 0 }
        ns.db.openKills[key] = k
    end
    return k
end

local function write(c, k)
    if k.kills == 0 and k.secretLines == 0 and k.questXp == 0 then return end
    local list = {}
    for name, x in pairs(k.creatures) do list[#list + 1] = { name = name, kills = x.kills, xp = x.xp } end
    table.sort(list, function(a, b)
        if a.kills ~= b.kills then return a.kills > b.kills end
        return a.name < b.name
    end)
    local top, other = {}, k.otherKills
    for i, x in ipairs(list) do
        if i <= TOP then top[#top + 1] = x else other = other + x.kills end
    end
    ns.SetState("kills", ns.StateKey(c) .. "|" .. k.loginAt, {
        sessionLoginAt = k.loginAt, kills = k.kills, xp = k.xp, restedXp = k.restedXp, groupKills = k.groupKills,
        instanceKills = k.instanceKills, instanceXp = k.instanceXp, secretLines = k.secretLines,
        questXp = k.questXp, firstAt = k.firstAt, lastAt = k.lastAt, creatures = top, otherKills = other,
    })
    lastWrite = GetTime()
end

local function prune(c)
    local bucket = ns.db.state.kills
    if not bucket then return end
    local prefix, cutoff = ns.StateKey(c) .. "|", time() - KEEP
    for key, r in pairs(bucket) do
        if key:sub(1, #prefix) == prefix and (tonumber(r.sessionLoginAt) or 0) < cutoff then bucket[key] = nil end
    end
end

ns.On("CHAT_MSG_COMBAT_XP_GAIN", function(text)
    if not ns.Enabled("kills") then return end
    local c = ns.Character()
    if not c then return end
    local k = open(c)
    if not k then return end
    if ns.IsSecret(text) or text == nil then
        k.secretLines = k.secretLines + 1
        return
    end
    local line = ns.ParseXPLine(text)
    if not line then return end
    if line.kind == "quest" then
        k.questXp = k.questXp + line.xp
        return
    end
    local now = time()
    k.kills, k.xp = k.kills + 1, k.xp + line.xp
    k.firstAt, k.lastAt = k.firstAt or now, now
    if line.rested then k.restedXp = k.restedXp + line.rested end
    if line.group then k.groupKills = k.groupKills + 1 end
    local _, kind = ns.Call("GetInstanceInfo")
    if kind == "party" or kind == "raid" then
        k.instanceKills, k.instanceXp = k.instanceKills + 1, k.instanceXp + line.xp
    end
    local name = ns.Clean(line.name, 60)
    local x = k.creatures[name]
    if not x and k.names >= MAX_NAMES then
        k.otherKills = k.otherKills + 1
    else
        if not x then
            x = { kills = 0, xp = 0 }
            k.creatures[name] = x
            k.names = k.names + 1
        end
        x.kills, x.xp = x.kills + 1, x.xp + line.xp
    end
    if GetTime() - lastWrite >= WRITE_EVERY or k.kills == 1 then write(c, k) end
end, "Kills.line")

ns.On("PLAYER_LOGOUT", function()
    local c = ns.Character()
    if not c or not ns.Enabled("kills") then return end
    local k = ns.db.openKills and ns.db.openKills[ns.StateKey(c)]
    if k then write(c, k) end
end, "Kills.logout")

ns.On("PLAYER_ENTERING_WORLD", function(isLogin)
    local c = ns.Character()
    if not c then return end
    prune(c)
    local k = ns.db.openKills and ns.db.openKills[ns.StateKey(c)]
    if isLogin and k and ns.SessionId and k.loginAt ~= ns.SessionId() then
        if ns.Enabled("kills") then write(c, k) end
        ns.db.openKills[ns.StateKey(c)] = nil
    end
end, "Kills.world")
