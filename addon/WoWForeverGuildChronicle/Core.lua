local addonName, ns = ...

ns.VERSION = "2.1.3"
ns.SCHEMA = 1
ns.HISTORY_DAYS = 35
ns.LOG_LINES = 300

ns.CATEGORIES = { "rankNames", "professions", "loot", "death", "played", "reputations", "gold", "sold",
    "level", "quest", "instance", "talents", "honor", "guildRoster", "guildLog", "guildInfo", "charter", "lootWon", "bgMatch",
    "inspect", "duel", "questsDone", "xp", "sighting", "calendar", "guildChat", "zones", "moments", "social", "dayCounts", "lockouts", "session", "groupSession", "fight", "lootHistory", "kills", "gear", "gearChange", "itemFacts", "charStats", "currencies", "inventory", "spellbook", "spellUse", "collections", "guildNews", "featsProbe", "probe" }
ns.LIMITS = { loot = 20000, death = 2000, gold = 40000, sold = 10000, level = 100, quest = 5000, instance = 5000,
    lootWon = 10000, bgMatch = 1000, inspect = 5000, duel = 2000, charter = 100, moments = 30000, social = 20000, xp = 10000,
    sighting = 30000, fight = 10000, gearChange = 10000, itemFacts = 10000 }

local frame = CreateFrame("Frame")
local handlers = {}
local early = {}

local function now()
    local d = date or os.date
    return d("%Y-%m-%d %H:%M:%S")
end

function ns.Log(level, msg)
    local line = now() .. " " .. level .. " " .. tostring(msg)
    if not ns.db then
        early[#early + 1] = line
        return
    end
    ns.Push(ns.db.diag.log, line, ns.LOG_LINES)
    if level == "error" and ns.ProbeError then ns.ProbeError(msg) end
    if ns.db.debugChat then ns.Print("|cff999999" .. level .. "|r " .. tostring(msg)) end
end

function ns.Debug(msg) ns.Log("debug", msg) end

function ns.NoteMissing(name)
    if not ns.db or ns.db.diag.missing[name] then return end
    ns.db.diag.missing[name] = true
    ns.Log("warn", "missing API " .. name)
end

ns.unknownEvents = {}
function ns.On(event, fn, name)
    if not handlers[event] then
        handlers[event] = {}
        if not pcall(frame.RegisterEvent, frame, event) then
            ns.unknownEvents[event] = true
            ns.Log("warn", "unknown event " .. event)
        end
    end
    table.insert(handlers[event], { fn = fn, name = name or "?" })
end

function ns.InCombat()
    if type(InCombatLockdown) ~= "function" then return false end
    local ok, v = pcall(InCombatLockdown)
    return ok and v == true
end

local owed, owedOrder = {}, {}
function ns.AfterCombat(key, fn)
    if not ns.InCombat() then
        fn()
        return true
    end
    if not owed[key] then owedOrder[#owedOrder + 1] = key end
    owed[key] = fn
    return false
end

ns.On("PLAYER_REGEN_ENABLED", function()
    local keys, fns = owedOrder, owed
    owed, owedOrder = {}, {}
    for _, key in ipairs(keys) do
        local ok, err = pcall(fns[key])
        if not ok then ns.Log("error", "after combat " .. key .. ": " .. tostring(err)) end
    end
end, "Core.afterCombat")

function ns.IsSecret(v)
    if type(issecretvalue) ~= "function" then return false end
    local ok, secret = pcall(issecretvalue, v)
    return ok and secret == true
end

ns.OnSecret = nil

local function secretSeen(where)
    if ns.OnSecret then pcall(ns.OnSecret, where) end
end

local function unsecret(kind, name, ...)
    if type(issecretvalue) ~= "function" then return ... end
    local n, args = select("#", ...), { ... }
    for i = 1, n do
        if ns.IsSecret(args[i]) then
            args[i] = nil
            secretSeen(kind .. ":" .. name .. "#" .. i)
        end
    end
    return unpack(args, 1, n)
end

frame:SetScript("OnEvent", function(_, event, ...)
    if ns.db then
        local counts = ns.db.diag.events
        counts[event] = (counts[event] or 0) + 1
    end
    for _, h in ipairs(handlers[event] or {}) do
        local ok, err = pcall(h.fn, unsecret("event", event, ...))
        if not ok then ns.Log("error", event .. " " .. h.name .. ": " .. tostring(err)) end
    end
end)

function ns.NoSecrets(v, where)
    if type(issecretvalue) ~= "function" then return v end
    if ns.IsSecret(v) then
        secretSeen("store:" .. tostring(where))
        return nil
    end
    if type(v) ~= "table" then return v end
    local out = {}
    for k, x in pairs(v) do
        if not ns.IsSecret(k) then out[k] = ns.NoSecrets(x, where) end
    end
    return out
end

ns.ALIASES = {
    GetItemInfo = { "C_Item", "GetItemInfo" },
    GetContainerNumSlots = { "C_Container", "GetContainerNumSlots" },
    GuildRoster = { "C_GuildInfo", "GuildRoster" },
}

function ns.Func(name)
    local space, member = name:match("^([%w_]+)%.([%w_]+)$")
    if space then
        local t = _G[space]
        local fn = type(t) == "table" and t[member]
        if type(fn) == "function" then return fn end
        return
    end
    local fn = _G[name]
    if type(fn) == "function" then return fn end
    local alias = ns.ALIASES[name]
    space = alias and _G[alias[1]]
    fn = type(space) == "table" and space[alias[2]]
    if type(fn) == "function" then return fn end
end

ns.recentCalls = {}
function ns.Trace(name)
    local r = ns.recentCalls
    r[#r + 1] = { name = name, t = type(GetTime) == "function" and GetTime() or 0 }
    if #r > 12 then table.remove(r, 1) end
end

function ns.Call(name, ...)
    ns.Trace(name)
    local fn = ns.Func(name)
    if type(fn) ~= "function" then
        ns.NoteMissing(name)
        return
    end
    local outer = ns.calling
    ns.calling = name
    local results = { pcall(fn, ...) }
    ns.calling = outer
    if not results[1] then
        ns.Log("error", name .. ": " .. tostring(results[2]))
        return
    end
    return unsecret("call", name, unpack(results, 2, table.maxn(results)))
end

function ns.Print(msg)
    DEFAULT_CHAT_FRAME:AddMessage("|cff66bbffWFT|r " .. msg)
end

function ns.Enabled(category)
    return ns.db ~= nil and ns.db.enabled[category] ~= false
end

function ns.Character()
    local name, second = UnitFullName("player")
    if not name then return nil end
    local realm = ns.Call("GetRealmName")
    if type(realm) ~= "string" then realm = "" end
    local _, classFile = UnitClass("player")
    local c = {
        name = name,
        realm = realm,
        guid = ns.PlayerGUID(),
        class = classFile,
        race = ns.PlayerRace(),
        level = UnitLevel("player"),
        client = ns.Client(),
    }
    if type(second) == "string" and second ~= "" and not ns.IsRealmName(second, realm) then
        c.surname = second
    end
    local guild = ns.Call("GetGuildInfo", "player")
    if type(guild) == "string" and guild ~= "" then
        c.guild = ns.Clean(guild, 60)
    elseif ns.Func("IsInGuild") and ns.Call("IsInGuild") == false then
        c.inGuild = false
    end
    return c
end

function ns.PlayerRace()
    local _, race = ns.Call("UnitRace", "player")
    if type(race) ~= "string" then return "" end
    return ns.Clean(race, 40) or ""
end

function ns.IsRealmName(s, realm)
    local normalized = ns.SafeCall(GetNormalizedRealmName)
    if type(normalized) == "string" and s == normalized then return true end
    return realm ~= "" and s == (realm:gsub("[%s%-]", ""))
end

function ns.SafeCall(fn, ...)
    if type(fn) ~= "function" then return nil end
    local ok, v = pcall(fn, ...)
    if not ok or ns.IsSecret(v) then return nil end
    return v
end

local client
function ns.Client()
    if client then return client end
    local c = {}
    if type(WOW_PROJECT_ID) == "number" then c.project = WOW_PROJECT_ID end
    local interface = select(4, ns.Call("GetBuildInfo"))
    if type(interface) == "number" then c.build = interface end
    local faction = ns.Call("UnitFactionGroup", "player")
    if type(faction) == "string" and faction ~= "" then c.faction = faction end
    if type(C_GameRules) == "table" then
        local mode = ns.SafeCall(C_GameRules.GetActiveGameMode)
        if type(mode) == "number" or type(mode) == "string" then c.gameMode = mode end
        local hc = ns.SafeCall(C_GameRules.IsHardcoreActive)
        if type(hc) == "boolean" then c.hardcore = hc end
    end
    local region = ns.SafeCall(GetCurrentRegion)
    if type(region) == "number" then c.region = region end
    local normalized = ns.SafeCall(GetNormalizedRealmName)
    if type(normalized) == "string" and normalized ~= "" then c.realmNormalized = normalized end
    if next(c) == nil then return nil end
    if c.faction then client = c end
    return c
end

function ns.PlayerGUID()
    local g = ns.SafeCall(UnitGUID, "player")
    if type(g) ~= "string" then return "" end
    return g
end

function ns.CharKey(c)
    return c.name .. "-" .. c.realm
end

function ns.StateKey(c)
    if type(c.surname) == "string" and c.surname ~= "" then
        return c.name .. " " .. c.surname .. "-" .. c.realm
    end
    return ns.CharKey(c)
end

ns.PER_CHARACTER_STATE = {
    "talents", "professions", "reputations", "honor", "played", "questsDone",
    "session", "kills", "groupSession", "lootHistory", "guildChat", "gear",
}

function ns.RekeyState(db)
    if type(db.state) ~= "table" then return end
    local moved = 0
    for _, category in ipairs(ns.PER_CHARACTER_STATE) do
        local bucket = db.state[category]
        if type(bucket) == "table" then
            local moves = {}
            for key, r in pairs(bucket) do
                local c = type(r) == "table" and r.character
                if type(key) == "string" and type(c) == "table" and type(c.name) == "string"
                    and type(c.realm) == "string" then
                    local old, new = ns.CharKey(c), ns.StateKey(c)
                    if old ~= new and key:sub(1, #old) == old and key:sub(1, #new) ~= new then
                        moves[#moves + 1] = { key, new .. key:sub(#old + 1) }
                    end
                end
            end
            for _, m in ipairs(moves) do
                local r, there = bucket[m[1]], bucket[m[2]]
                bucket[m[1]] = nil
                if not there or (tonumber(there.at) or 0) < (tonumber(r.at) or 0) then bucket[m[2]] = r end
                moved = moved + 1
            end
        end
    end
    if moved > 0 then ns.Log("info", "moved " .. moved .. " records to the keys with surnames") end
end

function ns.Who(c)
    if type(c.guid) == "string" and c.guid ~= "" then return c.guid end
    return "char:" .. ns.StateKey(c)
end

function ns.NextSeq()
    ns.db.seq = (ns.db.seq or 0) + 1
    return ns.db.seq
end

function ns.NewRecord(category, fields, key)
    local c = ns.Character()
    if not c then return nil end
    local r = ns.Copy(ns.NoSecrets(fields, category))
    r.type = category
    r.character = c
    r.at = r.at or time()
    r.addonVersion = ns.VERSION
    local basis
    if ns.LIMITS[category] then
        basis = ns.Who(c) .. "|" .. category .. "|" .. r.at .. "|" .. tostring(key or "")
    else
        basis = ns.Who(c) .. "|" .. category .. "|" .. ns.Serialize(fields) .. "|" .. r.at
    end
    r.id = category .. "-" .. ns.Hash(basis)
    return r
end

local function stateContent(r)
    local f = {}
    for k, v in pairs(r) do
        if k ~= "id" and k ~= "type" and k ~= "at" and k ~= "character" and k ~= "addonVersion" then f[k] = v end
    end
    return ns.Serialize(f)
end

function ns.AddHistory(category, fields, key)
    if not ns.Enabled(category) then return end
    local r = ns.NewRecord(category, fields, key)
    if not r then return end
    local byChar = ns.db.history[category] or {}
    ns.db.history[category] = byChar
    local key = ns.CharKey(r.character)
    byChar[key] = byChar[key] or {}
    ns.Push(byChar[key], r, ns.LIMITS[category])
    return r
end

function ns.SetState(category, stateKey, fields)
    if not ns.Enabled(category) then return end
    local bucket = ns.db.state[category] or {}
    ns.db.state[category] = bucket
    local r = ns.NewRecord(category, fields)
    if not r then return end
    local old = bucket[stateKey]
    if old and type(old.character) == "table" and old.character.guid == r.character.guid
        and stateContent(old) == stateContent(r) then
        return old
    end
    bucket[stateKey] = r
    return r
end

function ns.HistoryCount(category)
    local n = 0
    for _, list in pairs(ns.db.history[category] or {}) do n = n + #list end
    return n
end

local function initDB()
    local db = WoWForeverGuildChronicleDB
    if type(db) ~= "table" or db.schema ~= ns.SCHEMA then
        db = { schema = ns.SCHEMA }
    end
    db.enabled = type(db.enabled) == "table" and db.enabled or {}
    for _, cat in ipairs(ns.CATEGORIES) do db.enabled[cat] = true end
    db.appNoticeDay = nil
    db.history = db.history or {}
    db.state = db.state or {}
    db.recipes = db.recipes or {}
    db.seen = db.seen or {}
    db.probe = type(db.probe) == "table" and db.probe or {}
    db.probe.sessions = db.probe.sessions or {}
    db.diag = db.diag or {}
    db.diag.log = db.diag.log or {}
    db.diag.events = {}
    db.diag.missing = {}
    db.diag.version = ns.VERSION
    WoWForeverGuildChronicleDB = db
    ns.db = db

    for _, line in ipairs(early) do ns.Push(db.diag.log, line, ns.LOG_LINES) end
    early = {}

    for category in pairs(ns.LIMITS) do
        local h = db.history[category]
        if h and h[1] ~= nil then
            local byChar = {}
            for _, r in ipairs(h) do
                if type(r) == "table" and type(r.character) == "table" and r.character.name then
                    local key = r.character.name .. "-" .. tostring(r.character.realm)
                    byChar[key] = byChar[key] or {}
                    byChar[key][#byChar[key] + 1] = r
                end
            end
            db.history[category] = byChar
            ns.Log("info", "moved " .. #h .. " " .. category .. " records into per-character lists")
        end
    end

    for _, gone in ipairs({ "bossKill", "groupDeath" }) do
        if db.history[gone] or db.enabled[gone] ~= nil then
            db.history[gone], db.enabled[gone] = nil, nil
            ns.Log("info", "the " .. gone .. " category is gone; its records were removed")
        end
    end

    ns.RekeyState(db)

    local cutoff = time() - ns.HISTORY_DAYS * 86400
    for category in pairs(ns.LIMITS) do
        for key, list in pairs(db.history[category] or {}) do
            local removed = ns.PruneOlder(list, cutoff)
            if removed > 0 then ns.Log("info", "pruned " .. removed .. " " .. category .. " records older than " .. ns.HISTORY_DAYS .. " days") end
            if #list == 0 then db.history[category][key] = nil end
        end
    end
    for event in pairs(ns.unknownEvents) do db.diag.missing["event " .. event] = true end
    ns.Log("info", "loaded v" .. ns.VERSION)
end

ns.On("ADDON_LOADED", function(name)
    if name ~= addonName then return end
    local loadMs = ns.lastFileAt and debugprofilestop and math.floor(debugprofilestop() - ns.lastFileAt + 0.5) or nil
    local loadKB = collectgarbage and math.floor(collectgarbage("count")) or nil
    initDB()
    if ns.db and ns.db.diag then
        ns.db.diag.load = { ms = loadMs, kb = loadKB, at = date and date("%Y-%m-%d %H:%M:%S") or nil }
    end
end, "Core.init")
